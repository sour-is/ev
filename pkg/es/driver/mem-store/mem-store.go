package memstore

import (
	"context"
	"fmt"

	"github.com/sour-is/ev/internal/logz"
	"github.com/sour-is/ev/pkg/es"
	"github.com/sour-is/ev/pkg/es/driver"
	"github.com/sour-is/ev/pkg/es/event"
	"github.com/sour-is/ev/pkg/locker"
	"github.com/sour-is/ev/pkg/math"
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

const AppendOnly = es.AppendOnly
const AllEvents = es.AllEvents

func Init(ctx context.Context) {
	ctx, span := logz.Span(ctx)
	defer span.End()

	es.Register(ctx, "mem", &memstore{})
}

var _ driver.Driver = (*memstore)(nil)

func (memstore) Open(ctx context.Context, name string) (driver.Driver, error) {
	_, span := logz.Span(ctx)
	defer span.End()

	s := &state{streams: make(map[string]*locker.Locked[event.Events])}
	return &memstore{locker.New(s)}, nil
}
func (m *memstore) EventLog(ctx context.Context, streamID string) (driver.EventLog, error) {
	ctx, span := logz.Span(ctx)
	defer span.End()

	el := &eventLog{streamID: streamID}

	err := m.state.Modify(ctx, func(state *state) error {
		_, span := logz.Span(ctx)
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
	ctx, span := logz.Span(ctx)
	defer span.End()

	event.SetStreamID(m.streamID, events...)

	return uint64(len(events)), m.events.Modify(ctx, func(stream *event.Events) error {
		_, span := logz.Span(ctx)
		defer span.End()

		span.AddEvent(fmt.Sprintf(" %s %#v %d", m.streamID, stream, len(*stream)))

		last := uint64(len(*stream))
		if version != AppendOnly && version != last {
			return fmt.Errorf("current version wrong %d != %d", version, last)
		}

		for i := range events {
			span.AddEvent(fmt.Sprintf("read event %d of %d", i, len(events)))

			pos := last + uint64(i) + 1
			event.SetPosition(events[i], pos)
			*stream = append(*stream, events[i])
		}

		return nil
	})
}

// Read implements driver.EventStore
func (m *eventLog) Read(ctx context.Context, pos int64, count int64) (event.Events, error) {
	ctx, span := logz.Span(ctx)
	defer span.End()

	var events event.Events

	err := m.events.Modify(ctx, func(stream *event.Events) error {
		_, span := logz.Span(ctx)
		defer span.End()

		span.AddEvent(fmt.Sprintf(" %s %#v %d", m.streamID, stream, len(*stream)))

		first := stream.First().EventMeta().Position
		last := stream.Last().EventMeta().Position
		// ---
		if first == 0 || last == 0 {
			return nil
		}

		start, count := math.PagerBox(first, last, pos, count)
		if count == 0 {
			return nil
		}
		span.AddEvent(fmt.Sprint("box", first, last, pos, count))
		events = make([]event.Event, math.Abs(count))
		for i := range events {
			span.AddEvent(fmt.Sprintf("read event %d of %d", i, math.Abs(count)))
			// ---
			events[i] = (*stream)[start-1]
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

		return nil
	})
	if err != nil {
		return nil, err
	}

	event.SetStreamID(m.streamID, events...)

	return events, nil
}

// FirstIndex for the streamID
func (m *eventLog) FirstIndex(ctx context.Context) (uint64, error) {
	_, span := logz.Span(ctx)
	defer span.End()

	events, err := m.events.Copy(ctx)
	return events.First().EventMeta().Position, err
}

// LastIndex for the streamID
func (m *eventLog) LastIndex(ctx context.Context) (uint64, error) {
	_, span := logz.Span(ctx)
	defer span.End()

	events, err := m.events.Copy(ctx)
	return events.Last().EventMeta().Position, err
}

func (m *eventLog) LoadForUpdate(ctx context.Context, a event.Aggregate, fn func(context.Context, event.Aggregate) error) (uint64, error) {
	panic("not implemented")
}
