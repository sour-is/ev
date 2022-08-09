package memstore

import (
	"context"
	"fmt"

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
	es.Register(ctx, "mem", &memstore{})
}

var _ driver.Driver = (*memstore)(nil)

func (memstore) Open(_ context.Context, name string) (driver.Driver, error) {
	s := &state{streams: make(map[string]*locker.Locked[event.Events])}
	return &memstore{locker.New(s)}, nil
}
func (m *memstore) EventLog(ctx context.Context, streamID string) (driver.EventLog, error) {
	el := &eventLog{streamID: streamID}

	err := m.state.Modify(ctx, func(state *state) error {
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
	event.SetStreamID(m.streamID, events...)

	return uint64(len(events)), m.events.Modify(ctx, func(stream *event.Events) error {
		last := uint64(len(*stream))
		if version != AppendOnly && version != last {
			return fmt.Errorf("current version wrong %d != %d", version, last)
		}

		for i := range events {
			pos := last + uint64(i) + 1
			event.SetPosition(events[i], pos)
			*stream = append(*stream, events[i])
		}

		return nil
	})
}

// Read implements driver.EventStore
func (es *eventLog) Read(ctx context.Context, pos int64, count int64) (event.Events, error) {
	var events event.Events

	err := es.events.Modify(ctx, func(stream *event.Events) error {
		first := stream.First().EventMeta().Position
		last := stream.Last().EventMeta().Position
		// ---
		if first == 0 || last == 0 {
			return nil
		}

		if count == AllEvents {
			count = int64(first - last)
		}

		var start uint64

		switch {
		case pos >= 0 && count > 0:
			start = first + uint64(pos)
		case pos < 0 && count > 0:
			start = uint64(int64(last) + pos + 1)

		case pos >= 0 && count < 0:
			start = first + uint64(pos)
			if pos > 1 {
				start -= 2 // if pos is positive and count negative start before
			}
			if pos <= 1 {
				return nil // if pos is one or zero and negative count nothing to return
			}
		case pos < 0 && count < 0:
			start = uint64(int64(last) + pos)
		}
		if start >= last {
			return nil // if start is after last and positive count nothing to return
		}

		events = make([]event.Event, math.Abs(count))
		for i := range events {
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

	event.SetStreamID(es.streamID, events...)

	return events, nil
}

// FirstIndex for the streamID
func (m *eventLog) FirstIndex(ctx context.Context) (uint64, error) {
	events, err := m.events.Copy(ctx)
	return events.First().EventMeta().Position, err
}

// LastIndex for the streamID
func (m *eventLog) LastIndex(ctx context.Context) (uint64, error) {
	events, err := m.events.Copy(ctx)
	return events.Last().EventMeta().Position, err
}
