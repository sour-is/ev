// package memstore provides a driver that reads and writes events to memory.
package memstore

import (
	"context"
	"fmt"

	"go.sour.is/ev"
	"go.sour.is/ev/internal/lg"
	"go.sour.is/ev/pkg/es/driver"
	"go.sour.is/ev/pkg/es/event"
	"go.sour.is/ev/pkg/locker"
	"go.sour.is/ev/pkg/math"
)

type state struct {
	streams map[string]*locker.Locked[event.Events]
}
type eventLog struct {
	streamID string
	events   *locker.Locked[event.Events]
}
type memstore struct {
	state *locker.Locked[state]
}

const AppendOnly = ev.AppendOnly
const AllEvents = ev.AllEvents

func Init(ctx context.Context) error {
	ctx, span := lg.Span(ctx)
	defer span.End()

	return ev.Register(ctx, "mem", &memstore{})
}

var _ driver.Driver = (*memstore)(nil)

func (memstore) Open(ctx context.Context, name string) (driver.Driver, error) {
	_, span := lg.Span(ctx)
	defer span.End()

	s := &state{streams: make(map[string]*locker.Locked[event.Events])}
	return &memstore{locker.New(s)}, nil
}
func (m *memstore) EventLog(ctx context.Context, streamID string) (driver.EventLog, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	el := &eventLog{streamID: streamID}

	err := m.state.Modify(ctx, func(ctx context.Context, state *state) error {
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

var _ driver.EventLog = (*eventLog)(nil)

// Append implements driver.EventStore
func (m *eventLog) Append(ctx context.Context, events event.Events, version uint64) (uint64, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	event.SetStreamID(m.streamID, events...)

	return uint64(len(events)), m.events.Modify(ctx, func(ctx context.Context, stream *event.Events) error {
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

	var events event.Events
	err := m.events.Modify(ctx, func(ctx context.Context, stream *event.Events) error {
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

	var events event.Events

	err := m.events.Modify(ctx, func(ctx context.Context, stream *event.Events) error {
		ctx, span := lg.Span(ctx)
		defer span.End()

		span.AddEvent(fmt.Sprintf("%s %d", m.streamID, len(*stream)))

		first := stream.First().EventMeta().Position
		last := stream.Last().EventMeta().Position
		// ---
		if first == 0 || last == 0 {
			return nil
		}

		start, count := math.PagerBox(first, last, after, count)
		if count == 0 {
			return nil
		}
		span.AddEvent(fmt.Sprint("box", first, last, after, count))
		events = make([]event.Event, math.Abs(count))
		for i := range events {
			span.AddEvent(fmt.Sprintf("read event %d of %d", i, math.Abs(count)))

			// --- clone event
			var err error
			events[i], err = readStream(ctx, stream, start)
			if err != nil {
				return err
			}
			// ---

			if count > 0 {
				start += 1
			} else {
				start -= 1
			}
			if start < first || start > last {
				events = events[:i+1]
				break
			}
		}
		event.SetStreamID(m.streamID, events...)

		return nil
	})
	if err != nil {
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

func (m *eventLog) LoadForUpdate(ctx context.Context, a event.Aggregate, fn func(context.Context, event.Aggregate) error) (uint64, error) {
	panic("not implemented")
}

func readStream(ctx context.Context, stream *event.Events, index uint64) (event.Event, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	var b []byte
	var err error
	e := (*stream)[index-1]
	b, err = event.MarshalBinary(e)
	if err != nil {
		return nil, err
	}
	e, err = event.UnmarshalBinary(ctx, b, e.EventMeta().ActualPosition)
	if err != nil {
		return nil, err
	}
	return e, err
}
func readStreamN(ctx context.Context, stream *event.Events, index ...uint64) (event.Events, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	var b []byte
	var err error

	events := make(event.Events, len(index))
	for i, index := range index {
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
