package es

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/sour-is/ev/internal/logz"
	"github.com/sour-is/ev/pkg/es/driver"
	"github.com/sour-is/ev/pkg/es/event"
	"github.com/sour-is/ev/pkg/locker"
	"go.opentelemetry.io/otel/metric/instrument/syncint64"
	"go.uber.org/multierr"
)

type config struct {
	drivers map[string]driver.Driver
}

const AppendOnly = ^uint64(0)
const AllEvents = int64(AppendOnly >> 1)

var (
	drivers = locker.New(&config{drivers: make(map[string]driver.Driver)})
)

func Register(ctx context.Context, name string, d driver.Driver) error {
	ctx, span := logz.Span(ctx)
	defer span.End()

	m := logz.Meter(ctx)

	var err, errs error
	Mes_open, err = m.SyncInt64().Counter("es_open")
	errs = multierr.Append(errs, err)

	Mes_read, err = m.SyncInt64().Counter("es_read")
	errs = multierr.Append(errs, err)

	Mes_load, err = m.SyncInt64().Counter("es_load")
	errs = multierr.Append(errs, err)

	Mes_save, err = m.SyncInt64().Counter("es_save")
	errs = multierr.Append(errs, err)

	Mes_append, err = m.SyncInt64().Counter("es_append")
	errs = multierr.Append(errs, err)

	err = drivers.Modify(ctx, func(c *config) error {
		if _, set := c.drivers[name]; set {
			return fmt.Errorf("driver %s already set", name)
		}
		c.drivers[name] = d
		return nil
	})

	return multierr.Append(errs, err)
}

type EventStore struct {
	driver.Driver
}

var (
	Mes_open   syncint64.Counter
	Mes_read   syncint64.Counter
	Mes_load   syncint64.Counter
	Mes_save   syncint64.Counter
	Mes_append syncint64.Counter
)

func Open(ctx context.Context, dsn string, options ...Option) (*EventStore, error) {
	ctx, span := logz.Span(ctx)
	defer span.End()

	Mes_open.Add(ctx, 1)

	name, _, ok := strings.Cut(dsn, ":")
	if !ok {
		return nil, fmt.Errorf("%w: no scheme", ErrNoDriver)
	}

	var d driver.Driver
	c, err := drivers.Copy(ctx)
	if err != nil {
		return nil, err
	}

	d, ok = c.drivers[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s not registered", ErrNoDriver, name)
	}

	conn, err := d.Open(ctx, dsn)

	es := &EventStore{Driver: conn}
	for _, o := range options {
		o.Apply(es)
	}

	return es, err
}

type Option interface {
	Apply(*EventStore)
}

func (es *EventStore) Save(ctx context.Context, agg event.Aggregate) (uint64, error) {
	ctx, span := logz.Span(ctx)
	defer span.End()

	Mes_save.Add(ctx, 1)

	l, err := es.EventLog(ctx, agg.StreamID())
	if err != nil {
		return 0, err
	}
	events := agg.Events(true)

	count, err := l.Append(ctx, events, agg.StreamVersion())
	if err != nil {
		return 0, err
	}

	agg.Commit()
	return count, err
}
func (es *EventStore) Load(ctx context.Context, agg event.Aggregate) error {
	ctx, span := logz.Span(ctx)
	defer span.End()

	Mes_load.Add(ctx, 1)

	l, err := es.Driver.EventLog(ctx, agg.StreamID())
	if err != nil {
		return err
	}

	events, err := l.Read(ctx, 0, AllEvents)
	if err != nil {
		return err
	}
	event.Append(agg, events...)

	return nil
}
func (es *EventStore) Read(ctx context.Context, streamID string, pos, count int64) (event.Events, error) {
	ctx, span := logz.Span(ctx)
	defer span.End()

	Mes_read.Add(ctx, 1)

	l, err := es.Driver.EventLog(ctx, streamID)
	if err != nil {
		return nil, err
	}
	return l.Read(ctx, pos, count)
}
func (es *EventStore) Append(ctx context.Context, streamID string, events event.Events) (uint64, error) {
	ctx, span := logz.Span(ctx)
	defer span.End()

	Mes_append.Add(ctx, 1)

	l, err := es.Driver.EventLog(ctx, streamID)
	if err != nil {
		return 0, err
	}
	return l.Append(ctx, events, AppendOnly)
}
func (es *EventStore) FirstIndex(ctx context.Context, streamID string) (uint64, error) {
	ctx, span := logz.Span(ctx)
	defer span.End()

	l, err := es.Driver.EventLog(ctx, streamID)
	if err != nil {
		return 0, err
	}
	return l.FirstIndex(ctx)
}
func (es *EventStore) LastIndex(ctx context.Context, streamID string) (uint64, error) {
	ctx, span := logz.Span(ctx)
	defer span.End()

	l, err := es.Driver.EventLog(ctx, streamID)
	if err != nil {
		return 0, err
	}
	return l.LastIndex(ctx)
}

func (es *EventStore) EventStream() driver.EventStream {
	d := es.Driver
	for d != nil {
		if d, ok := d.(driver.EventStream); ok {
			return d
		}

		d = Unwrap(d)
	}
	return nil
}

func Unwrap[T any](t T) T {
	if unwrap, ok := any(t).(interface{ Unwrap() T }); ok {
		return unwrap.Unwrap()
	} else {
		var zero T
		return zero
	}
}

var ErrNoDriver = errors.New("no driver")
