package es_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sour-is/ev/pkg/es"
	memstore "github.com/sour-is/ev/pkg/es/driver/mem-store"
	"github.com/sour-is/ev/pkg/es/event"
)

func TestES(t *testing.T) {
	ctx := context.Background()

	event.Register(&ValueSet{})

	memstore.Init(ctx)

	es, err := es.Open(ctx, "mem:")
	if err != nil {
		t.Fatal(err)
	}

	thing := &Thing{Name: "time"}
	err = es.Load(ctx, thing)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(thing.StreamVersion(), thing.Name, thing.Value)

	err = thing.OnSetValue(time.Now().String())
	if err != nil {
		t.Fatal(err)
	}

	i, err := es.Save(ctx, thing)
	if err != nil {
		t.Fatal(err)
	}

	t.Log(thing.StreamVersion(), thing.Name, thing.Value)
	t.Log("Wrote: ", i)

	events, err := es.Read(ctx, "thing-time", -1, -11)
	if err != nil {
		t.Fatal(err)
	}

	for i, e := range events {
		t.Logf("event %d %d - %v\n", i, e.EventMeta().Position, e)
	}
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

var (
	_ event.Event     = (*ValueSet)(nil)
	_ event.Aggregate = (*Thing)(nil)
)
