package resolvelinks

import (
	"context"
	"errors"

	"go.sour.is/ev"
	"go.sour.is/ev/pkg/driver"
	"go.sour.is/ev/pkg/event"
	"go.sour.is/pkg/lg"
)

type resolvelinks struct {
	up driver.Driver
}

func New() *resolvelinks {
	return &resolvelinks{}
}

func (r *resolvelinks) Apply(es *ev.EventStore) {
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
	up       driver.EventLog
	resolver *resolvelinks
}

func (r *wrapper) Unwrap() driver.EventLog {
	return r.up
}

func (w *wrapper) Read(ctx context.Context, after int64, count int64) (event.Events, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	events, err := w.up.Read(ctx, after, count)
	if err != nil {
		return nil, err
	}

	err = w.resolvelinks(ctx, events)
	return events, err
}

func (w *wrapper) ReadN(ctx context.Context, index ...uint64) (event.Events, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	events, err := w.up.ReadN(ctx, index...)
	if err != nil {
		return nil, err
	}

	err = w.resolvelinks(ctx, events)
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

func (w *wrapper) resolvelinks(ctx context.Context, events event.Events) error {
	idx := make(map[string][]uint64)
	ptrs := make(map[string][]int)
	for i := range events {
		e := events[i]
		if e, ok := e.(*event.EventPtr); ok {
			idx[e.StreamID] = append(idx[e.StreamID], e.Pos)
			ptrs[e.StreamID] = append(ptrs[e.StreamID], i)
		}
	}

	for streamID, ids := range idx {
		d, err := w.resolver.EventLog(ctx, streamID)
		if err != nil {
			return err
		}
		ptr := ptrs[streamID]
		lis, err := d.ReadN(ctx, ids...)
		if err != nil && !errors.Is(err, ev.ErrNotFound) {
			return err
		}

		for i := range lis {
			meta := lis[i].EventMeta()
			actual := events[ptr[i]].EventMeta()
			meta.ActualPosition = actual.Position
			meta.ActualStreamID = actual.ActualStreamID
			lis[i].SetEventMeta(meta)
			events[i] = lis[i]
		}
	}
	return nil
}
