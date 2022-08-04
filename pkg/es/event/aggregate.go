package event

import (
	"errors"
	"fmt"
	"sync"
)

type Aggregate interface {
	// ApplyEvent  applies the event to the aggrigate state
	ApplyEvent(...Event)
	StreamID() string

	AggregateRootInterface
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

// CheckVersion returns an error if the version does not match.
func CheckVersion(a Aggregate, version uint64) error {
	if version != uint64(a.StreamVersion()) {
		return fmt.Errorf("version wrong, got (proposed) %d != (expected) %d", version, a.StreamVersion())
	}
	return nil
}

// NotExists returns error if there are no events present.
func NotExists(a Aggregate) error {
	if a.StreamVersion() != 0 {
		return fmt.Errorf("%w, got version == %d", ErrShouldNotExist, a.StreamVersion())
	}
	return nil
}

type AggregateRootInterface interface {
	// Events returns the aggrigate events
	// pass true for only uncommitted events
	Events(bool) Events
	// StreamVersion returns last commit events
	StreamVersion() uint64
	// Version returns the current aggrigate version. (committed + uncommitted)
	Version() uint64

	raise(lis ...Event)
	append(lis ...Event)
	Commit()
}

var _ AggregateRootInterface = &AggregateRoot{}

type AggregateRoot struct {
	events        Events
	streamVersion uint64

	mu sync.RWMutex
}

func (a *AggregateRoot) Commit() {
	a.streamVersion = uint64(len(a.events))
}

func (a *AggregateRoot) StreamVersion() uint64 {
	return a.streamVersion
}
func (a *AggregateRoot) Events(new bool) Events {
	a.mu.RLock()
	defer a.mu.RUnlock()

	events := a.events
	if new {
		events = events[a.streamVersion:]
	}

	lis := make(Events, len(events))
	copy(lis, events)

	return lis
}
func (a *AggregateRoot) Version() uint64 {
	return uint64(len(a.events))
}

//lint:ignore U1000 is called by embeded interface
func (a *AggregateRoot) raise(lis ...Event) { //nolint
	a.mu.Lock()
	defer a.mu.Unlock()

	a.posStartAt(lis...)

	a.events = append(a.events, lis...)
}

//lint:ignore U1000 is called by embeded interface
func (a *AggregateRoot) append(lis ...Event) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.posStartAt(lis...)

	a.events = append(a.events, lis...)
	a.streamVersion += uint64(len(lis))
}

func (a *AggregateRoot) posStartAt(lis ...Event) {
	for i, e := range lis {
		m := e.EventMeta()
		m.Position = a.streamVersion + uint64(i) + 1
		e.SetEventMeta(m)
	}
}

var ErrShouldNotExist = errors.New("should not exist")
