package diskstore

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/tidwall/wal"
	"go.opentelemetry.io/otel/metric/instrument/syncint64"
	"go.uber.org/multierr"

	"github.com/sour-is/ev/internal/logz"
	"github.com/sour-is/ev/pkg/cache"
	"github.com/sour-is/ev/pkg/es"
	"github.com/sour-is/ev/pkg/es/driver"
	"github.com/sour-is/ev/pkg/es/event"
	"github.com/sour-is/ev/pkg/locker"
	"github.com/sour-is/ev/pkg/math"
)

const CachSize = 1000

type lockedWal = locker.Locked[wal.Log]
type openlogs struct {
	logs *cache.Cache[string, *lockedWal]
}
type diskStore struct {
	path     string
	openlogs *locker.Locked[openlogs]

	Mdisk_open  syncint64.Counter
	Mdisk_evict syncint64.Counter
}

const AppendOnly = es.AppendOnly
const AllEvents = es.AllEvents

func Init(ctx context.Context) error {
	m := logz.Meter(ctx)
	var err, errs error

	Mdisk_open, err := m.SyncInt64().Counter("disk_open")
	errs = multierr.Append(errs, err)

	Mdisk_evict, err := m.SyncInt64().Counter("disk_evict")
	errs = multierr.Append(errs, err)

	es.Register(ctx, "file", &diskStore{
		Mdisk_open:  Mdisk_open,
		Mdisk_evict: Mdisk_evict,
	})

	return errs
}

var _ driver.Driver = (*diskStore)(nil)

func (d *diskStore) Open(ctx context.Context, dsn string) (driver.Driver, error) {
	ctx, span := logz.Span(ctx)
	defer span.End()

	d.Mdisk_open.Add(ctx, 1)

	scheme, path, ok := strings.Cut(dsn, ":")
	if !ok {
		return nil, fmt.Errorf("expected scheme")
	}

	if scheme != "file" {
		return nil, fmt.Errorf("expeted scheme=file, got=%s", scheme)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		err = os.MkdirAll(path, 0700)
		if err != nil {
			return nil, err
		}
	}
	c, err := cache.NewWithEvict(CachSize, func(ctx context.Context, s string, l *lockedWal) {
		l.Modify(ctx, func(w *wal.Log) error {
			// logz.Mdisk_evict.Add(ctx, 1)

			err := w.Close()
			if err != nil {
				log.Print(err)
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	logs := &openlogs{logs: c}
	return &diskStore{
		path:        path,
		openlogs:    locker.New(logs),
		Mdisk_open:  d.Mdisk_open,
		Mdisk_evict: d.Mdisk_evict,
	}, nil
}
func (ds *diskStore) EventLog(ctx context.Context, streamID string) (driver.EventLog, error) {
	el := &eventLog{streamID: streamID}

	return el, ds.openlogs.Modify(ctx, func(openlogs *openlogs) error {
		if events, ok := openlogs.logs.Get(streamID); ok {
			el.events = *events
			return nil
		}

		l, err := wal.Open(filepath.Join(ds.path, streamID), wal.DefaultOptions)
		if err != nil {
			return err
		}
		el.events = locker.New(l)
		openlogs.logs.Add(ctx, streamID, el.events)
		return nil
	})
}

type eventLog struct {
	streamID string
	events   *locker.Locked[wal.Log]
}

var _ driver.EventLog = (*eventLog)(nil)

func (es *eventLog) Append(ctx context.Context, events event.Events, version uint64) (uint64, error) {
	event.SetStreamID(es.streamID, events...)

	var count uint64
	err := es.events.Modify(ctx, func(l *wal.Log) error {
		last, err := l.LastIndex()
		if err != nil {
			return err
		}

		if version != AppendOnly && version != last {
			return fmt.Errorf("current version wrong %d != %d", version, last)
		}

		var b []byte

		batch := &wal.Batch{}
		for i, e := range events {
			b, err = event.MarshalText(e)
			if err != nil {
				return err
			}
			pos := last + uint64(i) + 1
			event.SetPosition(e, pos)

			batch.Write(pos, b)
		}

		count = uint64(len(events))
		return l.WriteBatch(batch)
	})

	return count, err
}
func (es *eventLog) Read(ctx context.Context, pos, count int64) (event.Events, error) {
	var events event.Events

	err := es.events.Modify(ctx, func(stream *wal.Log) error {
		first, err := stream.FirstIndex()
		if err != nil {
			return err
		}
		last, err := stream.LastIndex()
		if err != nil {
			return err
		}
		// ---
		if first == 0 || last == 0 {
			return nil
		}

		start, count := math.PagerBox(first, last, pos, count)
		log.Println("reading", first, last, pos, count, start)
		if count == 0 {
			return nil
		}

		events = make([]event.Event, math.Abs(count))
		for i := range events {
			// ---
			var b []byte
			b, err = stream.Read(start)
			if err != nil {
				return err
			}
			events[i], err = event.UnmarshalText(ctx, b, start)
			if err != nil {
				return err
			}
			// ---

			if count > 0 {
				start += 1
			} else {
				start -= 1
			}
			if start < first || start > last {
				events = events[:i+1]
				break
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	event.SetStreamID(es.streamID, events...)

	return events, err
}
func (es *eventLog) FirstIndex(ctx context.Context) (uint64, error) {
	var idx uint64
	var err error

	err = es.events.Modify(ctx, func(events *wal.Log) error {
		idx, err = events.FirstIndex()
		return err
	})

	return idx, err
}
func (es *eventLog) LastIndex(ctx context.Context) (uint64, error) {
	var idx uint64
	var err error

	err = es.events.Modify(ctx, func(events *wal.Log) error {
		idx, err = events.LastIndex()
		return err
	})

	return idx, err
}
