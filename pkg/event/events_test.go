package event_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/matryer/is"

	"go.sour.is/ev/pkg/event"
)

type DummyEvent struct {
	Value string

	event.IsEvent
}

func (e *DummyEvent) MarshalBinary() ([]byte, error) {
	return json.Marshal(e)
}
func (e *DummyEvent) UnmarshalBinary(b []byte) error {
	return json.Unmarshal(b, e)
}

func TestEventEncode(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	err := event.Register(ctx, &DummyEvent{})
	is.NoErr(err)

	var lis event.Events = event.NewEvents(
		&DummyEvent{Value: "testA"},
		&DummyEvent{Value: "testB"},
		&DummyEvent{Value: "testC"},
	)
	lis.SetStreamID("test")

	blis, err := event.EncodeEvents(lis...)
	is.NoErr(err)

	for _, b := range blis {
		sp := bytes.SplitN(b, []byte{'\t'}, 4)
		is.Equal(len(sp), 4)
		is.Equal(string(sp[1]), "test")
		is.Equal(string(sp[2]), "event_test.DummyEvent")
	}

	chk, err := event.DecodeEvents(ctx, blis...)
	is.NoErr(err)

	for i := range chk {
		is.Equal(lis[i], chk[i])
	}
}

type exampleAgg struct{ value string }

func (a *exampleAgg) ApplyEvent(lis ...event.Event) {
	for _, e := range lis {
		switch e := e.(type) {
		case interface{ Payload() exampleEvSetValue }:
			a.value = e.Payload().value
		}
	}
}

type exampleEvSetValue struct{ value string }

func TestApplyEventGeneric(t *testing.T) {
	payload := &exampleAgg{}
	var agg = event.AsAggregate(payload)

	agg.ApplyEvent(event.NewEvents(
		event.AsEvent(exampleEvSetValue{"hello"}),
	)...)

	is := is.New(t)
	is.Equal(payload.value, "hello")
}
