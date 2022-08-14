package event_test

import (
	"testing"

	"github.com/sour-is/ev/pkg/es/event"
)

type Agg struct {
	Value string

	event.AggregateRoot
}

var _ event.Aggregate = (*Agg)(nil)

func (a *Agg) streamID() string {
	return "value-" + a.Value
}

// ApplyEvent  applies the event to the aggrigate state
func (a *Agg) ApplyEvent(lis ...event.Event) {
	for _, e := range lis {
		switch e := e.(type) {
		case *ValueApplied:
			a.Value = e.Value
			a.SetStreamID(a.streamID())
		}
	}
}

type ValueApplied struct {
	Value string

	eventMeta event.Meta
}

var _ event.Event = (*ValueApplied)(nil)

func (e *ValueApplied) EventMeta() event.Meta {
	if e == nil {
		return event.Meta{}
	}
	return e.eventMeta
}

func (e *ValueApplied) SetEventMeta(m event.Meta) {
	if e != nil {
		e.eventMeta = m
	}
}

func TestAggregate(t *testing.T) {
	agg := &Agg{}
	event.Append(agg, &ValueApplied{Value: "one"})
}
