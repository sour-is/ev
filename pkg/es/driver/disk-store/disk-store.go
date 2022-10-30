// package diskstore provides a driver that reads and writes events to disk.

package diskstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tidwall/wal"
	"go.opentelemetry.io/otel/metric/instrument/syncint64"
	"go.uber.org/multierr"

	"github.com/sour-is/ev/internal/lg"
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

	m_disk_open  syncint64.Counter
	m_disk_evict syncint64.Counter
	m_disk_read  syncint64.Counter
	m_disk_write syncint64.Counter
}

const AppendOnly = es.AppendOnly
const AllEvents = es.AllEvents

func Init(ctx context.Context) error {
	_, span := lg.Span(ctx)
	defer span.End()

	d := &diskStore{}

	m := lg.Meter(ctx)
	var err, errs error

	d.m_disk_open, err = m.SyncInt64().Counter("disk_open")
	errs = multierr.Append(errs, err)

	d.m_disk_evict, err = m.SyncInt64().Counter("disk_evict")
	errs = multierr.Append(errs, err)

	d.m_disk_read, err = m.SyncInt64().Counter("disk_read")
	errs = multierr.Append(errs, err)

	d.m_disk_write, err = m.SyncInt64().Counter("disk_write")
	errs = multierr.Append(errs, err)

	es.Register(ctx, "file", d)

	return errs
}

var _ driver.Driver = (*diskStore)(nil)

func (d *diskStore) Open(ctx context.Context, dsn string) (driver.Driver, error) {
	_, span := lg.Span(ctx)
	defer span.End()

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
			span.RecordError(err)
			return nil, err
		}
	}
	c, err := cache.NewWithEvict(CachSize, func(ctx context.Context, s string, l *lockedWal) {
		_, span := lg.Span(ctx)
		defer span.End()

		l.Modify(ctx, func(ctx context.Context, w *wal.Log) error {
			_, span := lg.Span(ctx)
			defer span.End()

			d.m_disk_evict.Add(ctx, 1)

			err := w.Close()
			if err != nil {
				span.RecordError(err)
				return err
			}
			return nil
		})
	})
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	logs := &openlogs{logs: c}
	return &diskStore{
		path:         path,
		openlogs:     locker.New(logs),
		m_disk_open:  d.m_disk_open,
		m_disk_evict: d.m_disk_evict,
		m_disk_read:  d.m_disk_read,
		m_disk_write: d.m_disk_write,
	}, nil
}
func (d *diskStore) EventLog(ctx context.Context, streamID string) (driver.EventLog, error) {
	_, span := lg.Span(ctx)
	defer span.End()

	el := &eventLog{streamID: streamID, diskStore: d}

	return el, d.openlogs.Modify(ctx, func(ctx context.Context, openlogs *openlogs) error {
		_, span := lg.Span(ctx)
		defer span.End()

		if events, ok := openlogs.logs.Get(streamID); ok {
			el.events = *events
			return nil
		}

		d.m_disk_open.Add(ctx, 1)

		l, err := wal.Open(filepath.Join(d.path, streamID), wal.DefaultOptions)
		if err != nil {
			span.RecordError(err)
			return err
		}

		el.events = locker.New(l)
		openlogs.logs.Add(ctx, streamID, el.events)
		return nil
	})
}

type eventLog struct {
	streamID  string
	events    *locker.Locked[wal.Log]
	diskStore *diskStore
}

var _ driver.EventLog = (*eventLog)(nil)

func (e *eventLog) Append(ctx context.Context, events event.Events, version uint64) (uint64, error) {
	_, span := lg.Span(ctx)
	defer span.End()

	event.SetStreamID(e.streamID, events...)

	var count uint64
	err := e.events.Modify(ctx, func(ctx context.Context, l *wal.Log) error {
		_, span := lg.Span(ctx)
		defer span.End()

		last, err := l.LastIndex()
		if err != nil {
			span.RecordError(err)
			return err
		}

		if version != AppendOnly && version != last {
			err = fmt.Errorf("%w: current version wrong %d != %d", es.ErrWrongVersion, version, last)
			span.RecordError(err)
			return err
		}

		var b []byte

		batch := &wal.Batch{}
		for i, e := range events {
			span.AddEvent(fmt.Sprintf("append event %d of %d", i, len(events)))

			b, err = event.MarshalBinary(e)
			if err != nil {
				span.RecordError(err)

				return err
			}
			pos := last + uint64(i) + 1
			event.SetPosition(e, pos)

			batch.Write(pos, b)
		}

		count = uint64(len(events))
		e.diskStore.m_disk_write.Add(ctx, int64(len(events)))

		return l.WriteBatch(batch)
	})
	span.RecordError(err)

	return count, err
}
func (e *eventLog) Read(ctx context.Context, after, count int64) (event.Events, error) {
	_, span := lg.Span(ctx)
	defer span.End()

	var events event.Events

	err := e.events.Modify(ctx, func(ctx context.Context, stream *wal.Log) error {
		_, span := lg.Span(ctx)
		defer span.End()

		first, err := stream.FirstIndex()
		if err != nil {
			span.RecordError(err)
			return err
		}
		last, err := stream.LastIndex()
		if err != nil {
			span.RecordError(err)
			return err
		}
		// ---
		if first == 0 || last == 0 {
			return nil
		}

		start, count := math.PagerBox(first, last, after, count)
		if count == 0 {
			return nil
		}

		events = make([]event.Event, math.Abs(count))
		for i := range events {
			span.AddEvent(fmt.Sprintf("read event %d of %d", i, len(events)))

			// ---
			events[i], err = readStream(ctx, stream, start)
			if err != nil {
				span.RecordError(err)
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
		span.RecordError(err)
		return nil, err
	}

	event.SetStreamID(e.streamID, events...)
	e.diskStore.m_disk_read.Add(ctx, int64(len(events)))

	return events, nil
}
func (e *eventLog) ReadN(ctx context.Context, index ...uint64) (event.Events, error) {
	_, span := lg.Span(ctx)
	defer span.End()

	var events event.Events
	err := e.events.Modify(ctx, func(ctx context.Context, stream *wal.Log) error {
		var err error

		events, err = readStreamN(ctx, stream, index...)

		return err
	})

	return events, err
}
func (e *eventLog) FirstIndex(ctx context.Context) (uint64, error) {
	_, span := lg.Span(ctx)
	defer span.End()

	var idx uint64
	var err error

	err = e.events.Modify(ctx, func(ctx context.Context, events *wal.Log) error {
		idx, err = events.FirstIndex()
		return err
	})

	return idx, err
}
func (e *eventLog) LastIndex(ctx context.Context) (uint64, error) {
	_, span := lg.Span(ctx)
	defer span.End()

	var idx uint64
	var err error

	err = e.events.Modify(ctx, func(ctx context.Context, events *wal.Log) error {
		idx, err = events.LastIndex()
		return err
	})

	return idx, err
}
func (e *eventLog) LoadForUpdate(ctx context.Context, a event.Aggregate, fn func(context.Context, event.Aggregate) error) (uint64, error) {
	panic("not implemented")
}
func readStream(ctx context.Context, stream *wal.Log, index uint64) (event.Event, error) {
	_, span := lg.Span(ctx)
	defer span.End()

	var b []byte
	var err error
	b, err = stream.Read(index)
	if err != nil {
		if errors.Is(err, wal.ErrNotFound) || errors.Is(err, wal.ErrOutOfRange) {
			err = fmt.Errorf("%w: empty", es.ErrNotFound)
		}

		span.RecordError(err)
		return nil, err
	}
	e, err := event.UnmarshalBinary(ctx, b, index)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	return e, err
}
func readStreamN(ctx context.Context, stream *wal.Log, index ...uint64) (event.Events, error) {
	_, span := lg.Span(ctx)
	defer span.End()

	var b []byte
	var err error
	events := make(event.Events, len(index))
	for i, idx := range index {
		b, err = stream.Read(idx)
		if err != nil {
			if errors.Is(err, wal.ErrNotFound) || errors.Is(err, wal.ErrOutOfRange) {
				err = fmt.Errorf("%w: empty", es.ErrNotFound)
			}

			span.RecordError(err)
			return nil, err
		}
		events[i], err = event.UnmarshalBinary(ctx, b, idx)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
	}
	return events, err
}