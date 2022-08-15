package es_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/matryer/is"

	"github.com/sour-is/ev/pkg/es"
	memstore "github.com/sour-is/ev/pkg/es/driver/mem-store"
	"github.com/sour-is/ev/pkg/es/event"
)

func TestES(t *testing.T) {
	is := is.New(t)

	ctx := context.Background()

	err := event.Register(ctx, &ValueSet{})
	is.NoErr(err)

	memstore.Init(ctx)

	_, err = es.Open(ctx, "mem")
	is.True(errors.Is(err, es.ErrNoDriver))

	_, err = es.Open(ctx, "bogo:")
	is.True(errors.Is(err, es.ErrNoDriver))

	es, err := es.Open(ctx, "mem:")
	is.NoErr(err)

	thing := &Thing{Name: "time"}
	err = es.Load(ctx, thing)
	is.NoErr(err)

	t.Log(thing.StreamVersion(), thing.Name, thing.Value)

	err = thing.OnSetValue(time.Now().String())
	is.NoErr(err)

	i, err := es.Save(ctx, thing)
	is.NoErr(err)

	t.Log(thing.StreamVersion(), thing.Name, thing.Value)
	t.Log("Wrote: ", i)

	i, err = es.Append(ctx, "thing-time", event.NewEvents(&ValueSet{Value: "xxx"}))
	is.NoErr(err)
	is.Equal(i, uint64(1))

	events, err := es.Read(ctx, "thing-time", -1, -11)
	is.NoErr(err)

	for i, e := range events {
		t.Logf("event %d %d - %v\n", i, e.EventMeta().Position, e)
	}

	first, err := es.FirstIndex(ctx, "thing-time")
	is.NoErr(err)
	is.Equal(first, uint64(1))

	last, err := es.LastIndex(ctx, "thing-time")
	is.NoErr(err)
	is.Equal(last, uint64(2))

}

type Thing struct {
	Name  string
	Value string

	event.AggregateRoot
}

func (a *Thing) StreamID() string {
	return fmt.Sprintf("thing-%s", a.Name)
}
func (a *Thing) ApplyEvent(lis ...event.Event) {
	for _, e := range lis {
		switch e := e.(type) {
		case *ValueSet:
			a.Value = e.Value
		}
	}
}
func (a *Thing) OnSetValue(value string) error {
	event.Raise(a, &ValueSet{Value: value})
	return nil
}

type ValueSet struct {
	Value string

	eventMeta event.Meta
}

func (e *ValueSet) EventMeta() event.Meta {
	if e == nil {
		return event.Meta{}
	}
	return e.eventMeta
}
func (e *ValueSet) SetEventMeta(eventMeta event.Meta) {
	if e == nil {
		return
	}
	e.eventMeta = eventMeta
}
func (e *ValueSet) MarshalBinary() ([]byte, error) {
	return json.Marshal(e)
}
func (e *ValueSet) UnmarshalBinary(b []byte) error {
	return json.Unmarshal(b, e)
}

var (
	_ event.Event     = (*ValueSet)(nil)
	_ event.Aggregate = (*Thing)(nil)
)
