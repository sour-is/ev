package event

import (
	"errors"
	"fmt"
	"sync"
)

// Aggregate implements functionality for working with event store streams as an aggregate.
// When creating a new Aggregate the struct should have an ApplyEvent method and embed the AggregateRoot.
type Aggregate interface {
	// ApplyEvent  applies the event to the aggrigate state
	ApplyEvent(...Event)

	AggregateRoot
}

func Start(a Aggregate, i uint64) {
	a.start(i)
}

// Raise adds new uncommitted events
func Raise(a Aggregate, lis ...Event) {
	lis = NewEvents(lis...)
	SetStreamID(a.StreamID(), lis...)
	a.raise(lis...)
	a.ApplyEvent(lis...)
}

// Append adds new committed events
func Append(a Aggregate, lis ...Event) {
	a.append(lis...)
	a.ApplyEvent(lis...)
}

// NotExists returns error if there are no events present.
func NotExists(a Aggregate) error {
	if a.Version() != 0 {
		return fmt.Errorf("%w, got version == %d", ErrShouldNotExist, a.Version())
	}
	return nil
}

// ShouldExists returns error if there are no events present.
func ShouldExist(a Aggregate) error {
	if a.Version() == 0 {
		return fmt.Errorf("%w, got version == %d", ErrShouldExist, a.Version())
	}
	return nil
}

type AggregateRoot interface {
	// Events returns the aggregate events
	// pass true for only uncommitted events
	Events(bool) Events
	// StreamID returns aggregate stream ID
	StreamID() string
	// SetStreamID sets aggregate stream ID
	SetStreamID(streamID string)
	// StreamVersion returns last commit events
	StreamVersion() uint64
	// Version returns the current aggrigate version. (committed + uncommitted)
	Version() uint64

	start(uint64)
	raise(lis ...Event)
	append(lis ...Event)
	Commit()
}

var _ AggregateRoot = &IsAggregate{}

type IsAggregate struct {
	events     Events
	streamID   string
	firstIndex uint64
	lastIndex  uint64

	mu sync.RWMutex
}

func (a *IsAggregate) Commit()                     { a.lastIndex = uint64(len(a.events)) }
func (a *IsAggregate) StreamID() string            { return a.streamID }
func (a *IsAggregate) SetStreamID(streamID string) { a.streamID = streamID }
func (a *IsAggregate) StreamVersion() uint64       { return a.lastIndex }
func (a *IsAggregate) Version() uint64             { return a.firstIndex + uint64(len(a.events)) }
func (a *IsAggregate) Events(new bool) Events {
	a.mu.RLock()
	defer a.mu.RUnlock()

	events := a.events
	if new {
		events = events[a.lastIndex-a.firstIndex:]
	}

	lis := make(Events, len(events))
	copy(lis, events)

	return lis
}

func (a *IsAggregate) start(i uint64) {
	a.firstIndex = i
	a.lastIndex = i
}

//lint:ignore U1000 is called by embeded interface
func (a *IsAggregate) raise(lis ...Event) { //nolint
	a.mu.Lock()
	defer a.mu.Unlock()

	a.posStartAt(lis...)

	a.events = append(a.events, lis...)
}

//lint:ignore U1000 is called by embeded interface
func (a *IsAggregate) append(lis ...Event) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.posStartAt(lis...)

	a.events = append(a.events, lis...)
	a.lastIndex += uint64(len(lis))
}

func (a *IsAggregate) posStartAt(lis ...Event) {
	for i, e := range lis {
		m := e.EventMeta()
		m.Position = a.lastIndex + uint64(i) + 1
		e.SetEventMeta(m)
	}
}

var ErrShouldNotExist = errors.New("should not exist")
var ErrShouldExist = errors.New("should exist")
