// package projecter provides a driver middleware to derive new events from other events.
package projecter

import (
	"context"
	"strings"

	"github.com/sour-is/ev/internal/lg"
	"github.com/sour-is/ev/pkg/es"
	"github.com/sour-is/ev/pkg/es/driver"
	"github.com/sour-is/ev/pkg/es/event"
)

type projector struct {
	up  driver.Driver
	fns []func(event.Event) []event.Event
}

func New(ctx context.Context, fns ...func(event.Event) []event.Event) *projector {
	return &projector{fns: fns}
}
func (p *projector) Apply(e *es.EventStore) {
	p.up = e.Driver
	e.Driver = p
}
func (s *projector) Unwrap() driver.Driver {
	return s.up
}
func (s *projector) Open(ctx context.Context, dsn string) (driver.Driver, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	return s.up.Open(ctx, dsn)
}
func (s *projector) EventLog(ctx context.Context, streamID string) (driver.EventLog, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	l, err := s.up.EventLog(ctx, streamID)
	return &wrapper{l, s}, err
}

type wrapper struct {
	up        driver.EventLog
	projector *projector
}

var _ driver.EventLog = (*wrapper)(nil)

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

	{
		ctx, span := lg.Fork(ctx)

		go func() {
			defer span.End()

			var pevents []event.Event

			for i := range events {
				e := events[i]

				for _, fn := range w.projector.fns {
					pevents = append(
						pevents,
						fn(e)...,
					)
				}
			}

			for i := range pevents {
				e := pevents[i]
				l, err := w.projector.up.EventLog(ctx, event.StreamID(e).StreamID())
				if err != nil {
					span.RecordError(err)
					continue
				}
				_, err = l.Append(ctx, event.NewEvents(e), es.AppendOnly)
				span.RecordError(err)
			}
		}()
	}

	return i, err
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
func (w *wrapper) LoadForUpdate(ctx context.Context, a event.Aggregate, fn func(context.Context, event.Aggregate) error) (uint64, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	up := w.up
	for up != nil {
		if up, ok := up.(driver.EventLogWithUpdate); ok {
			return up.LoadForUpdate(ctx, a, fn)
		}
		up = es.Unwrap(up)
	}
	return 0, es.ErrNoDriver
}

func DefaultProjection(e event.Event) []event.Event {
	m := e.EventMeta()
	streamID := m.StreamID
	streamPos := m.Position
	eventType := event.TypeOf(e)
	pkg, _, _ := strings.Cut(eventType, ".")

	e1 := event.NewPtr(streamID, streamPos)
	event.SetStreamID("$all", e1)

	e2 := event.NewPtr(streamID, streamPos)
	event.SetStreamID("$type-"+eventType, e2)

	e3 := event.NewPtr(streamID, streamPos)
	event.SetStreamID("$pkg-"+pkg, e3)

	return []event.Event{e1, e2, e3}
}
