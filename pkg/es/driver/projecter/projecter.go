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
	up driver.Driver
}

func New(ctx context.Context) *projector {
	return &projector{}
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

func (w *wrapper) Read(ctx context.Context, pos int64, count int64) (event.Events, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	return w.up.Read(ctx, pos, count)
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
				eventType := event.TypeOf(e)
				m := e.EventMeta()
				streamID := m.StreamID
				streamPos := m.Position

				e1 := event.NewPtr(streamID, streamPos)
				event.SetStreamID("$all", e1)

				e2 := event.NewPtr(streamID, streamPos)
				event.SetStreamID("$type-"+eventType, e2)

				e3 := event.NewPtr(streamID, streamPos)
				pkg, _, _ := strings.Cut(eventType, ".")
				event.SetStreamID("$pkg-"+pkg, e3)

				pevents = append(
					pevents,
					e1,
					e2,
					e3,
				)
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

	return w.up.LoadForUpdate(ctx, a, fn)
}
