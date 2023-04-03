package ev_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/matryer/is"
	"go.uber.org/multierr"

	"go.sour.is/ev"
	"go.sour.is/ev/app/peerfinder"
	memstore "go.sour.is/ev/pkg/es/driver/mem-store"
	"go.sour.is/ev/pkg/es/driver/projecter"
	resolvelinks "go.sour.is/ev/pkg/es/driver/resolve-links"
	"go.sour.is/ev/pkg/es/driver/streamer"
	"go.sour.is/ev/pkg/es/event"
)

var (
	_ event.Event     = (*ValueSet)(nil)
	_ event.Aggregate = (*Thing)(nil)
)

type Thing struct {
	Name  string
	Value string

	event.IsAggregate
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

	event.IsEvent
}

func TestES(t *testing.T) {
	is := is.New(t)

	ctx := context.Background()

	err := event.Register(ctx, &ValueSet{})
	is.NoErr(err)

	{
		store, err := ev.Open(ctx, "mem")
		is.True(errors.Is(err, ev.ErrNoDriver))
		is.True(store.EventStream() == nil)
	}

	{
		_, err := ev.Open(ctx, "bogo:")
		is.True(errors.Is(err, ev.ErrNoDriver))
	}

	store, err := ev.Open(ctx, "mem:", streamer.New(ctx), projecter.New(ctx))
	is.NoErr(err)

	thing := &Thing{Name: "time"}
	err = store.Load(ctx, thing)
	is.True(errors.Is(err, ev.ErrNotFound))

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

	store, err := ev.Open(ctx, "mem:", streamer.New(ctx), projecter.New(ctx))
	is.NoErr(err)

	thing, err := ev.Create(ctx, store, "thing-1", func(ctx context.Context, agg *Thing) error {
		return agg.OnSetValue("foo")
	})

	is.NoErr(err)
	is.Equal(thing.Version(), uint64(1))
	is.Equal(thing.Value, "foo")

	thing, err = ev.Update(ctx, store, "thing-1", func(ctx context.Context, agg *Thing) error {
		return agg.OnSetValue("bar")
	})

	is.NoErr(err)
	is.Equal(thing.Version(), uint64(2))
	is.Equal(thing.Value, "bar")

	thing, err = ev.Upsert(ctx, store, "thing-2", func(ctx context.Context, agg *Thing) error {
		return agg.OnSetValue("bin")
	})

	is.NoErr(err)
	is.Equal(thing.Version(), uint64(1))
	is.Equal(thing.Value, "bin")

	thing, err = ev.Upsert(ctx, store, "thing-2", func(ctx context.Context, agg *Thing) error {
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

	is.Equal(ev.Unwrap(werr), err)
	is.Equal(ev.Unwrap("test"), "")
}

func TestUnwrapProjector(t *testing.T) {
	is := is.New(t)

	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	es, err := ev.Open(
		ctx,
		"mem:",
		resolvelinks.New(),
		streamer.New(ctx),
		projecter.New(
			ctx,
			projecter.DefaultProjection,
			peerfinder.Projector,
		),
	)
	is.NoErr(err)

	stream := es.EventStream()
	is.True(stream != nil)

}

func TestMain(m *testing.M) {
	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	err := multierr.Combine(
		ev.Init(ctx),
		event.Init(ctx),
		memstore.Init(ctx),
	)
	if err != nil {
		fmt.Println(err)
		return
	}

	m.Run()
}
