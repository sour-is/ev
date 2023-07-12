package event_test

import (
	"testing"

	"go.sour.is/ev/pkg/event"
)

type Agg struct {
	Value string

	event.IsAggregate
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

	event.IsEvent
}

var _ event.Event = (*ValueApplied)(nil)

func TestAggregate(t *testing.T) {
	agg := &Agg{}
	event.Append(agg, &ValueApplied{Value: "one"})
}
