package event_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/matryer/is"

	"github.com/sour-is/ev/pkg/es/event"
)

type DummyEvent struct {
	Value string

	eventMeta event.Meta
}

func (e *DummyEvent) EventMeta() event.Meta {
	if e == nil {
		return event.Meta{}
	}
	return e.eventMeta
}
func (e *DummyEvent) SetEventMeta(eventMeta event.Meta) {
	if e == nil {
		return
	}
	e.eventMeta = eventMeta
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
