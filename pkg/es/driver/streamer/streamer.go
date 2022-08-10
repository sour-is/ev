package streamer

import (
	"context"

	"github.com/sour-is/ev/pkg/es"
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
	return &streamer{state: locker.New(&state{subscribers: map[string][]*subscription{}})}
}

var _ es.Option = (*streamer)(nil)

func (s *streamer) Apply(e *es.EventStore) {
	s.up = e.Driver
	e.Driver = s
}
func (s *streamer) Unwrap() driver.Driver {
	return s.up
}

var _ driver.Driver = (*streamer)(nil)

func (s *streamer) Open(ctx context.Context, dsn string) (driver.Driver, error) {
	return s.up.Open(ctx, dsn)
}
func (s *streamer) EventLog(ctx context.Context, streamID string) (driver.EventLog, error) {
	l, err := s.up.EventLog(ctx, streamID)
	return &wrapper{streamID, l, s}, err
}

var _ driver.EventStream = (*streamer)(nil)

func (s *streamer) Subscribe(ctx context.Context, streamID string, start int64) (driver.Subscription, error) {
	events, err := s.up.EventLog(ctx, streamID)
	if err != nil {
		return nil, err
	}
	sub := &subscription{topic: streamID, events: events}
	sub.position = locker.New(&position{
		idx:  start,
		size: es.AllEvents,
	})
	sub.unsub = s.delete(streamID, sub)

	return sub, s.state.Modify(ctx, func(state *state) error {
		state.subscribers[streamID] = append(state.subscribers[streamID], sub)
		return nil
	})
}
func (s *streamer) Send(ctx context.Context, streamID string, events event.Events) error {
	return s.state.Modify(ctx, func(state *state) error {
		for _, sub := range state.subscribers[streamID] {
			return sub.position.Modify(ctx, func(position *position) error {
				position.size = int64(events.Last().EventMeta().Position - uint64(position.idx))

				if position.wait != nil {
					close(position.wait)
					position.wait = nil
				}
				return nil
			})
		}
		return nil
	})
}

func (s *streamer) delete(streamID string, sub *subscription) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return s.state.Modify(ctx, func(state *state) error {
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

func (w *wrapper) Read(ctx context.Context, pos int64, count int64) (event.Events, error) {
	return w.up.Read(ctx, pos, count)
}

func (w *wrapper) Append(ctx context.Context, events event.Events, version uint64) (uint64, error) {
	i, err := w.up.Append(ctx, events, version)
	if err != nil {
		return i, err
	}
	return i, w.streamer.Send(ctx, w.topic, events)
}

func (w *wrapper) FirstIndex(ctx context.Context) (uint64, error) {
	return w.up.FirstIndex(ctx)
}

func (w *wrapper) LastIndex(ctx context.Context) (uint64, error) {
	return w.up.LastIndex(ctx)
}

type position struct {
	size int64
	idx  int64
	wait chan struct{}
}

type subscription struct {
	topic string

	position *locker.Locked[position]

	events driver.EventLog
	unsub  func(context.Context) error
}

func (s *subscription) Recv(ctx context.Context) bool {
	var wait func(context.Context) bool
	err := s.position.Modify(ctx, func(position *position) error {
		if position.size == es.AllEvents {
			return nil
		}
		if position.size == 0 {
			position.wait = make(chan struct{})
			wait = func(ctx context.Context) bool {
				select {
				case <-position.wait:

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
		return false
	}

	if wait != nil {
		return wait(ctx)
	}

	return true
}
func (s *subscription) Events(ctx context.Context) (event.Events, error) {
	var events event.Events
	return events, s.position.Modify(ctx, func(position *position) error {
		var err error
		events, err = s.events.Read(ctx, int64(position.idx), position.size)
		position.size = int64(len(events))
		if len(events) > 0 {
			position.idx = int64(events.First().EventMeta().Position - 1)
		}
		return err
	})
}
func (s *subscription) Close(ctx context.Context) error {
	return s.unsub(ctx)
}