// package memstore provides a driver that reads and writes events to memory.
package memstore

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.sour.is/pkg/lg"
	"go.sour.is/pkg/locker"

	"go.sour.is/ev"
	"go.sour.is/ev/driver"
	"go.sour.is/ev/event"
)

const AppendOnly = ev.AppendOnly
const AllEvents = ev.AllEvents

type state struct {
	streams map[string]*locker.Locked[*event.Events]
}
type memstore struct {
	state *locker.Locked[*state]
}

var _ driver.Driver = (*memstore)(nil)

func Init(ctx context.Context) error {
	ctx, span := lg.Span(ctx)
	defer span.End()

	return ev.Register(ctx, "mem", &memstore{})
}

func (memstore) Open(ctx context.Context, name string) (driver.Driver, error) {
	_, span := lg.Span(ctx)
	defer span.End()

	s := &state{streams: make(map[string]*locker.Locked[*event.Events])}
	return &memstore{locker.New(s)}, nil
}
func (m *memstore) EventLog(ctx context.Context, streamID string) (driver.EventLog, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	el := &eventLog{streamID: streamID}

	err := m.state.Use(ctx, func(ctx context.Context, state *state) error {
		_, span := lg.Span(ctx)
		defer span.End()

		l, ok := state.streams[streamID]
		if !ok {
			l = locker.New(&event.Events{})
			state.streams[streamID] = l
		}
		el.events = l
		return nil
	})
	if err != nil {
		return nil, err
	}
	return el, err
}

type eventLog struct {
	streamID string
	events   *locker.Locked[*event.Events]
}

var _ driver.EventLog = (*eventLog)(nil)

// Append implements driver.EventStore
func (m *eventLog) Append(ctx context.Context, events event.Events, version uint64) (uint64, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	event.SetStreamID(m.streamID, events...)

	return uint64(len(events)), m.events.Use(ctx, func(ctx context.Context, stream *event.Events) error {
		ctx, span := lg.Span(ctx)
		defer span.End()

		span.AddEvent(fmt.Sprintf(" %s %d", m.streamID, len(*stream)))

		last := uint64(len(*stream))
		if version != AppendOnly && version != last {
			return fmt.Errorf("%w: current version wrong %d != %d", ev.ErrWrongVersion, version, last)
		}

		for i := range events {
			span.AddEvent(fmt.Sprintf("read event %d of %d", i, len(events)))

			// --- clone event
			e := events[i]
			b, err := event.MarshalBinary(e)
			if err != nil {
				return err
			}
			e, err = event.UnmarshalBinary(ctx, b, e.EventMeta().Position)
			if err != nil {
				return err
			}
			// ---

			pos := last + uint64(i) + 1
			event.SetPosition(e, pos)
			*stream = append(*stream, e)
		}

		return nil
	})
}

// ReadOne implements readone
func (m *eventLog) ReadN(ctx context.Context, index ...uint64) (event.Events, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	lis := make([]int64, len(index))
	for i := range index {
		lis[i] = int64(index[i])
	}

	span.SetAttributes(
		attribute.Int64Slice("args.index", lis),
		attribute.String("streamID", m.streamID),
	)

	var events event.Events
	err := m.events.Use(ctx, func(ctx context.Context, stream *event.Events) error {
		var err error

		events, err = readStreamN(ctx, stream, index...)

		return err
	})

	return events, err
}

// Read implements driver.EventStore
func (m *eventLog) Read(ctx context.Context, after int64, count int64) (event.Events, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()
	span.SetAttributes(
		attribute.Int64("args.after", after),
		attribute.Int64("args.count", count),
		attribute.String("streamID", m.streamID),
	)

	var events event.Events
	err := m.events.Use(ctx, func(ctx context.Context, stream *event.Events) error {
		ctx, span := lg.Span(ctx)
		defer span.End()

		first := stream.First().EventMeta().Position
		last := stream.Last().EventMeta().Position

		streamIDs, err := driver.GenerateStreamIDs(first, last, after, count)
		if err != nil {
			return err
		}
		events, err = readStreamN(ctx, stream, streamIDs...)
		event.SetStreamID(m.streamID, events...)
		return err
	})
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	return events, nil
}

// FirstIndex for the streamID
func (m *eventLog) FirstIndex(ctx context.Context) (uint64, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	events, err := m.events.Copy(ctx)
	return events.First().EventMeta().Position, err
}

// LastIndex for the streamID
func (m *eventLog) LastIndex(ctx context.Context) (uint64, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	events, err := m.events.Copy(ctx)
	return events.Last().EventMeta().Position, err
}

func (e *eventLog) Truncate(ctx context.Context, index int64) error {
	ctx, span := lg.Span(ctx)
	defer span.End()

	span.SetAttributes(
		attribute.Int64("args.index", index),
		attribute.String("streamID", e.streamID),
	)

	if index == 0 {
		return nil
	}
	return e.events.Use(ctx, func(ctx context.Context, events *event.Events) error {
		if index < 0 {
			*events = (*events)[:index]
			return nil
		}
		*events = (*events)[index:]
		return nil
	})
}
func readStreamN(ctx context.Context, stream *event.Events, index ...uint64) (event.Events, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	var b []byte
	var err error

	count := len(index)
	events := make(event.Events, count)
	for i, index := range index {
		span.AddEvent(fmt.Sprintf("read event %d of %d", i, count))

		e := (*stream)[index-1]
		b, err = event.MarshalBinary(e)
		if err != nil {
			return nil, err
		}
		events[i], err = event.UnmarshalBinary(ctx, b, e.EventMeta().Position)
		if err != nil {
			return nil, err
		}
	}
	return events, err
}
