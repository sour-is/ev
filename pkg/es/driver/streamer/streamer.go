// package streamer provides a driver to allow awaiting for new events to be added to a stream.
package streamer

import (
	"context"
	"fmt"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/sour-is/ev"
	"github.com/sour-is/ev/internal/lg"
	"github.com/sour-is/ev/pkg/es/driver"
	"github.com/sour-is/ev/pkg/es/event"
	"github.com/sour-is/ev/pkg/locker"
)

type state struct {
	subscribers map[string][]*subscription
}

type streamer struct {
	state *locker.Locked[state]
	up    driver.Driver
}

func New(ctx context.Context) *streamer {
	_, span := lg.Span(ctx)
	defer span.End()

	return &streamer{state: locker.New(&state{subscribers: map[string][]*subscription{}})}
}

var _ ev.Option = (*streamer)(nil)

func (s *streamer) Apply(e *ev.EventStore) {
	s.up = e.Driver
	e.Driver = s
}
func (s *streamer) Unwrap() driver.Driver {
	return s.up
}

var _ driver.Driver = (*streamer)(nil)

func (s *streamer) Open(ctx context.Context, dsn string) (driver.Driver, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	return s.up.Open(ctx, dsn)
}
func (s *streamer) EventLog(ctx context.Context, streamID string) (driver.EventLog, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	l, err := s.up.EventLog(ctx, streamID)
	return &wrapper{streamID, l, s}, err
}

var _ driver.EventStream = (*streamer)(nil)

func (s *streamer) Subscribe(ctx context.Context, streamID string, start int64) (driver.Subscription, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	events, err := s.up.EventLog(ctx, streamID)
	if err != nil {
		return nil, err
	}

	sub := &subscription{topic: streamID, events: events}
	sub.position = locker.New(&position{
		idx:  start,
		size: ev.AllEvents,
	})
	sub.unsub = s.delete(streamID, sub)

	return sub, s.state.Modify(ctx, func(ctx context.Context, state *state) error {
		state.subscribers[streamID] = append(state.subscribers[streamID], sub)
		return nil
	})
}
func (s *streamer) Send(ctx context.Context, streamID string, events event.Events) error {
	ctx, span := lg.Span(ctx)
	defer span.End()

	return s.state.Modify(ctx, func(ctx context.Context, state *state) error {
		ctx, span := lg.Span(ctx)
		defer span.End()

		span.AddEvent(fmt.Sprint("subscribers=", len(state.subscribers[streamID])))

		for _, sub := range state.subscribers[streamID] {
			err := sub.position.Modify(ctx, func(ctx context.Context, position *position) error {
				ctx, span := lg.Span(ctx)
				defer span.End()

				span.SetAttributes(
					attribute.String("streamID", streamID),
					attribute.Int64("actualPosition", int64(events.Last().EventMeta().ActualPosition)),
					attribute.String("actualStreamID", events.Last().EventMeta().ActualStreamID),
					attribute.Int64("position", int64(events.Last().EventMeta().Position)),
				)

				position.size = int64(events.Last().EventMeta().ActualPosition - uint64(position.idx))

				if position.wait != nil {
					close(position.wait)
					position.link = trace.LinkFromContext(ctx, attribute.String("src", "event"))
					position.wait = nil
				}

				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
}
func (s *streamer) delete(streamID string, sub *subscription) func(context.Context) error {
	return func(ctx context.Context) error {
		ctx, span := lg.Span(ctx)
		defer span.End()

		if err := ctx.Err(); err != nil {
			return err
		}
		return s.state.Modify(ctx, func(ctx context.Context, state *state) error {
			_, span := lg.Span(ctx)
			defer span.End()

			lis := state.subscribers[streamID]
			for i := range lis {
				if lis[i] == sub {
					lis[i] = lis[len(lis)-1]
					state.subscribers[streamID] = lis[:len(lis)-1]

					return nil
				}
			}
			return nil
		})
	}
}

type wrapper struct {
	topic    string
	up       driver.EventLog
	streamer *streamer
}

var _ driver.EventLog = (*wrapper)(nil)

func (r *wrapper) Unwrap() driver.EventLog {
	return r.up
}
func (w *wrapper) Read(ctx context.Context, after int64, count int64) (event.Events, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	return w.up.Read(ctx, after, count)
}
func (w *wrapper) ReadN(ctx context.Context, index ...uint64) (event.Events, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	return w.up.ReadN(ctx, index...)
}
func (w *wrapper) Append(ctx context.Context, events event.Events, version uint64) (uint64, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	i, err := w.up.Append(ctx, events, version)
	if err != nil {
		return i, err
	}

	ctx, span = lg.Fork(ctx)
	go func() {
		defer span.End()

		err := w.streamer.Send(ctx, w.topic, events)
		span.RecordError(err)
	}()

	return i, nil
}
func (w *wrapper) FirstIndex(ctx context.Context) (uint64, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	return w.up.FirstIndex(ctx)
}
func (w *wrapper) LastIndex(ctx context.Context) (uint64, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	return w.up.LastIndex(ctx)
}

type position struct {
	size int64
	idx  int64
	link trace.Link
	wait chan struct{}
}

type subscription struct {
	topic string

	position *locker.Locked[position]

	events driver.EventLog
	unsub  func(context.Context) error
	once   sync.Once
}

func (s *subscription) Recv(ctx context.Context) <-chan bool {
	ctx, span := lg.Span(ctx)
	defer span.End()

	done := make(chan bool)

	go func() {
		var wait func(context.Context) bool
		defer close(done)

		err := s.position.Modify(ctx, func(ctx context.Context, position *position) error {
			_, span := lg.Span(ctx)
			defer span.End()

			if position.size == ev.AllEvents {
				return nil
			}
			if position.size == 0 {
				position.wait = make(chan struct{})
				wait = func(ctx context.Context) bool {
					ctx, span := lg.Span(ctx)
					defer span.End()

					select {
					case <-position.wait:
						if position.link.SpanContext.IsValid() {
							_, span := lg.Span(ctx, trace.WithLinks(position.link))
							span.AddEvent("recv event")
							span.End()
							position.link = trace.Link{}
						}
						return true

					case <-ctx.Done():
						return false
					}
				}
			}
			position.idx += position.size
			position.size = 0
			return nil
		})
		if err != nil {
			done <- false
			return
		}

		if wait != nil {
			done <- wait(ctx)
			return
		}

		done <- true
	}()

	return done
}
func (s *subscription) Events(ctx context.Context) (event.Events, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	var events event.Events
	return events, s.position.Modify(ctx, func(ctx context.Context, position *position) error {
		ctx, span := lg.Span(ctx)
		defer span.End()

		var err error
		events, err = s.events.Read(ctx, position.idx, position.size)
		if err != nil {
			return err
		}
		position.size = int64(len(events))
		if len(events) > 0 {
			position.idx = int64(events.First().EventMeta().ActualPosition - 1)
		}
		span.SetAttributes(
			attribute.Int64("position.idx", position.idx),
			attribute.Int64("position.size", position.size),
			attribute.Int64("meta.ActualPosition", int64(events.First().EventMeta().ActualPosition)),
			attribute.Int64("meta.Position", int64(events.First().EventMeta().Position)),
		)

		return err
	})
}
func (s *subscription) Close(ctx context.Context) error {
	ctx, span := lg.Span(ctx)
	defer span.End()

	if s == nil || s.unsub == nil {
		return nil
	}

	var err error
	s.once.Do(func() { err = s.unsub(ctx) })

	return err
}
