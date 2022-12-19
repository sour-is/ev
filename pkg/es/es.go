// package es implements an event store and drivers for extending its functionality.
package es

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric/instrument/syncint64"
	"go.uber.org/multierr"

	"github.com/sour-is/ev/internal/lg"
	"github.com/sour-is/ev/pkg/es/driver"
	"github.com/sour-is/ev/pkg/es/event"
	"github.com/sour-is/ev/pkg/locker"
)

type config struct {
	drivers map[string]driver.Driver
}

const AppendOnly = ^uint64(0)
const AllEvents = int64(AppendOnly >> 1)

var (
	drivers = locker.New(&config{drivers: make(map[string]driver.Driver)})
)

func Init(ctx context.Context) error {
	m := lg.Meter(ctx)

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

	return errs
}

func Register(ctx context.Context, name string, d driver.Driver) error {
	ctx, span := lg.Span(ctx)
	defer span.End()

	return drivers.Modify(ctx, func(ctx context.Context, c *config) error {
		if _, set := c.drivers[name]; set {
			return fmt.Errorf("driver %s already set", name)
		}
		c.drivers[name] = d
		return nil
	})
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
	ctx, span := lg.Span(ctx)
	defer span.End()

	span.SetAttributes(attribute.String("dsn", dsn))

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
	es.Option(options...)

	Mes_open.Add(ctx, 1)

	return es, err
}

func (es *EventStore) Option(options ...Option) {
	for _, o := range options {
		o.Apply(es)
	}
}

type Option interface {
	Apply(*EventStore)
}

func (es *EventStore) Save(ctx context.Context, agg event.Aggregate) (uint64, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	events := agg.Events(true)
	if len(events) == 0 {
		return 0, nil
	}

	span.SetAttributes(
		attribute.String("agg.type", event.TypeOf(agg)),
		attribute.String("agg.streamID", agg.StreamID()),
		attribute.Int64("agg.version", int64(agg.StreamVersion())),
	)

	l, err := es.EventLog(ctx, agg.StreamID())
	if err != nil {
		return 0, err
	}

	count, err := l.Append(ctx, events, agg.StreamVersion())
	if err != nil {
		return 0, err
	}
	Mes_save.Add(ctx, int64(count))

	agg.Commit()
	return count, err
}
func (es *EventStore) Load(ctx context.Context, agg event.Aggregate) error {
	ctx, span := lg.Span(ctx)
	defer span.End()

	span.SetAttributes(
		attribute.String("agg.type", event.TypeOf(agg)),
		attribute.String("agg.streamID", agg.StreamID()),
	)

	l, err := es.Driver.EventLog(ctx, agg.StreamID())
	if err != nil {
		return err
	}

	events, err := l.Read(ctx, 0, AllEvents)
	if err != nil {
		return err
	}

	Mes_load.Add(ctx, events.Count())
	event.Append(agg, events...)

	span.SetAttributes(
		attribute.Int64("agg.version", int64(agg.StreamVersion())),
	)

	return nil
}
func (es *EventStore) Read(ctx context.Context, streamID string, after, count int64) (event.Events, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	span.SetAttributes(
		attribute.String("streamID", streamID),
		attribute.Int64("after", after),
		attribute.Int64("count", count),
	)

	l, err := es.Driver.EventLog(ctx, streamID)
	if err != nil {
		return nil, err
	}

	events, err := l.Read(ctx, after, count)
	Mes_read.Add(ctx, events.Count())

	return events, err
}
func (es *EventStore) ReadN(ctx context.Context, streamID string, index ...uint64) (event.Events, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	lis := make([]int64, len(index))
	for i, j := range index {
		lis[i] = int64(j)
	}

	span.SetAttributes(
		attribute.String("streamID", streamID),
		attribute.Int64Slice("index", lis),
	)

	l, err := es.Driver.EventLog(ctx, streamID)
	if err != nil {
		return nil, err
	}

	events, err := l.ReadN(ctx, index...)
	Mes_read.Add(ctx, events.Count())

	return events, err
}
func (es *EventStore) Append(ctx context.Context, streamID string, events event.Events) (uint64, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	Mes_append.Add(ctx, 1)
	span.SetAttributes(
		attribute.String("ev.streamID", streamID),
	)

	l, err := es.Driver.EventLog(ctx, streamID)
	if err != nil {
		return 0, err
	}
	return l.Append(ctx, events, AppendOnly)
}
func (es *EventStore) FirstIndex(ctx context.Context, streamID string) (uint64, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	span.SetAttributes(
		attribute.String("ev.streamID", streamID),
	)

	l, err := es.Driver.EventLog(ctx, streamID)
	if err != nil {
		return 0, err
	}
	return l.FirstIndex(ctx)
}
func (es *EventStore) LastIndex(ctx context.Context, streamID string) (uint64, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	span.SetAttributes(
		attribute.String("ev.streamID", streamID),
	)

	l, err := es.Driver.EventLog(ctx, streamID)
	if err != nil {
		return 0, err
	}
	return l.LastIndex(ctx)
}
func (es *EventStore) EventStream() driver.EventStream {
	if es == nil {
		return nil
	}
	d := es.Driver
	for d != nil {
		if d, ok := d.(driver.EventStream); ok {
			return d
		}

		d = Unwrap(d)
	}
	return nil
}
func (es *EventStore) Truncate(ctx context.Context, streamID string, index int64) error {
	ctx, span := lg.Span(ctx)
	defer span.End()

	up, err := es.Driver.EventLog(ctx, streamID)
	if err != nil {
		return err
	}

	for up != nil {
		if up, ok := up.(driver.EventLogWithTruncate); ok {
			err = up.Truncate(ctx, index)
			if err != nil {
				return err
			}
		}
		up = Unwrap(up)
	}

	return ErrNoDriver
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
var ErrWrongVersion = errors.New("wrong version")
var ErrShouldExist = event.ErrShouldExist
var ErrShouldNotExist = event.ErrShouldNotExist
var ErrNotFound = errors.New("not found")

type PA[T any] interface {
	event.Aggregate
	*T
}
type PE[T any] interface {
	event.Event
	*T
}

// Create uses fn to create a new aggregate and store in db.
func Create[A any, T PA[A]](ctx context.Context, es *EventStore, streamID string, fn func(context.Context, T) error) (agg T, err error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	agg = new(A)
	agg.SetStreamID(streamID)
	span.SetAttributes(
		attribute.String("agg.streamID", streamID),
	)

	if err = es.Load(ctx, agg); err != nil {
		return
	}

	if err = event.NotExists(agg); err != nil {
		return
	}

	if err = fn(ctx, agg); err != nil {
		return
	}

	var i uint64
	if i, err = es.Save(ctx, agg); err != nil {
		return
	}

	span.AddEvent(fmt.Sprint("wrote events = ", i))

	return
}

// Update uses fn to update an exsisting aggregate and store in db.
func Update[A any, T PA[A]](ctx context.Context, es *EventStore, streamID string, fn func(context.Context, T) error) (agg T, err error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	agg = new(A)
	agg.SetStreamID(streamID)
	span.SetAttributes(
		attribute.String("agg.streamID", streamID),
	)

	if err = es.Load(ctx, agg); err != nil {
		return
	}

	if err = event.ShouldExist(agg); err != nil {
		return
	}

	if err = fn(ctx, agg); err != nil {
		return
	}

	if _, err = es.Save(ctx, agg); err != nil {
		return
	}

	return
}

// Update uses fn to update an exsisting aggregate and store in db.
func Upsert[A any, T PA[A]](ctx context.Context, es *EventStore, streamID string, fn func(context.Context, T) error) (agg T, err error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	agg = new(A)
	agg.SetStreamID(streamID)
	span.SetAttributes(
		attribute.String("agg.streamID", streamID),
	)

	if err = es.Load(ctx, agg); err != nil {
		return
	}

	if err = fn(ctx, agg); err != nil {
		return
	}

	if err = event.ShouldExist(agg); err != nil {
		return
	}

	if _, err = es.Save(ctx, agg); err != nil {
		return
	}

	return
}
