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
	"github.com/sour-is/ev/pkg/es/driver/projecter"
	"github.com/sour-is/ev/pkg/es/driver/streamer"
	"github.com/sour-is/ev/pkg/es/event"
)

var (
	_ event.Event     = (*ValueSet)(nil)
	_ event.Aggregate = (*Thing)(nil)
)

type Thing struct {
	Name  string
	Value string

	event.AggregateRoot
}

//	func (a *Thing) StreamID() string {
//		return fmt.Sprintf("thing-%s", a.Name)
//	}
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

func TestES(t *testing.T) {
	is := is.New(t)

	ctx := context.Background()

	err := event.Register(ctx, &ValueSet{})
	is.NoErr(err)

	es.Init(ctx)
	memstore.Init(ctx)

	{
		store, err := es.Open(ctx, "mem")
		is.True(errors.Is(err, es.ErrNoDriver))
		is.True(store.EventStream() == nil)
	}

	{
		_, err := es.Open(ctx, "bogo:")
		is.True(errors.Is(err, es.ErrNoDriver))
	}

	store, err := es.Open(ctx, "mem:", streamer.New(ctx), projecter.New(ctx))
	is.NoErr(err)

	thing := &Thing{Name: "time"}
	err = store.Load(ctx, thing)
	is.NoErr(err)

	t.Log(thing.StreamVersion(), thing.Name, thing.Value)

	err = thing.OnSetValue(time.Now().String())
	is.NoErr(err)

	thing.SetStreamID("thing-time")
	i, err := store.Save(ctx, thing)
	is.NoErr(err)

	t.Log(thing.StreamVersion(), thing.Name, thing.Value)
	t.Log("Wrote: ", i)

	i, err = store.Append(ctx, "thing-time", event.NewEvents(&ValueSet{Value: "xxx"}))
	is.NoErr(err)
	is.Equal(i, uint64(1))

	events, err := store.Read(ctx, "thing-time", -1, -11)
	is.NoErr(err)

	for i, e := range events {
		t.Logf("event %d %d - %v\n", i, e.EventMeta().Position, e)
	}

	first, err := store.FirstIndex(ctx, "thing-time")
	is.NoErr(err)
	is.Equal(first, uint64(1))

	last, err := store.LastIndex(ctx, "thing-time")
	is.NoErr(err)
	is.Equal(last, uint64(2))

	stream := store.EventStream()
	is.True(stream != nil)
}

func TestESOperations(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	es.Init(ctx)
	memstore.Init(ctx)

	store, err := es.Open(ctx, "mem:", streamer.New(ctx), projecter.New(ctx))
	is.NoErr(err)

	thing, err := es.Create(ctx, store, "thing-1", func(ctx context.Context, agg *Thing) error {
		return agg.OnSetValue("foo")
	})

	is.NoErr(err)
	is.Equal(thing.Version(), uint64(1))
	is.Equal(thing.Value, "foo")

	thing, err = es.Update(ctx, store, "thing-1", func(ctx context.Context, agg *Thing) error {
		return agg.OnSetValue("bar")
	})

	is.NoErr(err)
	is.Equal(thing.Version(), uint64(2))
	is.Equal(thing.Value, "bar")

	thing, err = es.Upsert(ctx, store, "thing-2", func(ctx context.Context, agg *Thing) error {
		return agg.OnSetValue("bin")
	})

	is.NoErr(err)
	is.Equal(thing.Version(), uint64(1))
	is.Equal(thing.Value, "bin")

	thing, err = es.Upsert(ctx, store, "thing-2", func(ctx context.Context, agg *Thing) error {
		return agg.OnSetValue("baz")
	})

	is.NoErr(err)
	is.Equal(thing.Version(), uint64(2))
	is.Equal(thing.Value, "baz")

}

func TestUnwrap(t *testing.T) {
	is := is.New(t)

	err := errors.New("foo")
	werr := fmt.Errorf("wrap: %w", err)

	is.Equal(es.Unwrap(werr), err)
	is.Equal(es.Unwrap("test"), "")
}
