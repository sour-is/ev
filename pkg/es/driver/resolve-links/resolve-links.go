package resolvelinks

import (
	"context"

	"github.com/sour-is/ev/internal/lg"
	"github.com/sour-is/ev/pkg/es"
	"github.com/sour-is/ev/pkg/es/driver"
	"github.com/sour-is/ev/pkg/es/event"
)

type resolvelinks struct {
	up driver.Driver
}

func New() *resolvelinks {
	return &resolvelinks{}
}
func (r *resolvelinks) Apply(es *es.EventStore) {
	r.up = es.Driver
	es.Driver = r
}
func (r *resolvelinks) Unwrap() driver.Driver {
	return r.up
}
func (r *resolvelinks) Open(ctx context.Context, dsn string) (driver.Driver, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	return r.up.Open(ctx, dsn)
}

func (r *resolvelinks) EventLog(ctx context.Context, streamID string) (driver.EventLog, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	l, err := r.up.EventLog(ctx, streamID)
	return &wrapper{l, r}, err
}

type wrapper struct {
	up           driver.EventLog
	resolvelinks *resolvelinks
}

func (w *wrapper) Read(ctx context.Context, after int64, count int64) (event.Events, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	events, err := w.up.Read(ctx, after, count)
	if err != nil {
		return nil, err
	}

	for i, e := range events {
		switch e := e.(type) {
		case *event.EventPtr:
			d, err := w.resolvelinks.EventLog(ctx, e.StreamID)
			if err != nil {
				return nil, err
			}
			lis, err := d.ReadN(ctx, e.Pos)
			if err != nil {
				return nil, err
			}

			events[i] = lis.First()
		}
	}

	return events, err
}

func (w *wrapper) ReadN(ctx context.Context, index ...uint64) (event.Events, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	events, err := w.up.ReadN(ctx, index...)
	if err != nil {
		return nil, err
	}

	for i, e := range events {
		switch e := e.(type) {
		case *event.EventPtr:
			d, err := w.resolvelinks.EventLog(ctx, e.StreamID)
			if err != nil {
				return nil, err
			}
			lis, err := d.ReadN(ctx, e.Pos)
			if err != nil {
				return nil, err
			}

			events[i] = lis.First()
		}
	}

	return events, err
}

func (w *wrapper) Append(ctx context.Context, events event.Events, version uint64) (uint64, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	return w.up.Append(ctx, events, version)
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
