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
	streams map[string]event.Events
}

type memstore struct {
	state *locker.Locked[state]
}

var _ driver.Driver = (*memstore)(nil)

func Init(ctx context.Context) {
	es.Register(ctx, "mem", &memstore{})
}

func (memstore) Open(name string) (driver.EventStore, error) {
	s := &state{streams: make(map[string]event.Events)}
	return &memstore{locker.New(s)}, nil
}

// Append implements driver.EventStore
func (m *memstore) Append(ctx context.Context, streamID string, events event.Events) (uint64, error) {
	event.SetStreamID(streamID, events...)

	return uint64(len(events)), m.state.Modify(ctx, func(state *state) error {
		stream := state.streams[streamID]
		last := uint64(len(stream))
		for i := range events {
			pos := last + uint64(i) + 1
			event.SetPosition(events[i], pos)
			stream = append(stream, events[i])
			state.streams[streamID] = stream
		}

		return nil
	})
}

// Load implements driver.EventStore
func (m *memstore) Load(ctx context.Context, agg event.Aggregate) error {
	return m.state.Modify(ctx, func(state *state) error {
		events := state.streams[agg.StreamID()]
		event.SetStreamID(agg.StreamID(), events...)
		agg.ApplyEvent(events...)
		return nil
	})
}

// Read implements driver.EventStore
func (m *memstore) Read(ctx context.Context, streamID string, pos int64, count int64) (event.Events, error) {
	events := make([]event.Event, math.Abs(count))

	err := m.state.Modify(ctx, func(state *state) error {
		stream := state.streams[streamID]

		var first, last, start uint64
		first = stream.First().EventMeta().Position
		last = stream.Last().EventMeta().Position

		if first == 0 || last == 0 {
			events = events[:0]

			return nil
		}

		switch {
		case pos >= 0:
			start = first + uint64(pos)
			if pos == 0 && count < 0 {
				count = -count // if pos=0 assume forward count.
			}
		case pos < 0:
			start = uint64(int64(last) + pos + 1)
			if pos == -1 && count > 0 {
				count = -count // if pos=-1 assume backward count.
			}
		}

		for i := range events {
			events[i] = stream[start-1]

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

	return events, nil
}

// Save implements driver.EventStore
func (m *memstore) Save(ctx context.Context, agg event.Aggregate) (uint64, error) {
	events := agg.Events(true)
	event.SetStreamID(agg.StreamID(), events...)

	err := m.state.Modify(ctx, func(state *state) error {
		stream := state.streams[agg.StreamID()]

		last := uint64(len(stream))
		if agg.StreamVersion() != last {
			return fmt.Errorf("current version wrong %d != %d", agg.StreamVersion(), last)
		}

		for i := range events {
			pos := last + uint64(i) + 1
			event.SetPosition(events[i], pos)
			stream = append(stream, events[i])
		}

		state.streams[agg.StreamID()] = stream
		return nil
	})
	if err != nil {
		return 0, err
	}
	agg.Commit()

	return uint64(len(events)), nil
}
