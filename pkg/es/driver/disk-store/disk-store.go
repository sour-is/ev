package diskstore

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/tidwall/wal"

	"github.com/sour-is/ev/pkg/es"
	"github.com/sour-is/ev/pkg/es/driver"
	"github.com/sour-is/ev/pkg/es/event"
	"github.com/sour-is/ev/pkg/locker"
	"github.com/sour-is/ev/pkg/math"
)

type diskStore struct {
	path string
}

const AppendOnly = es.AppendOnly
const AllEvents = es.AllEvents

func Init(ctx context.Context) error {
	es.Register(ctx, "file", &diskStore{})
	return nil
}

var _ driver.Driver = (*diskStore)(nil)

func (diskStore) Open(_ context.Context, dsn string) (driver.Driver, error) {
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

	return &diskStore{path: path}, nil
}
func (ds *diskStore) EventLog(ctx context.Context, streamID string) (driver.EventLog, error) {
	el := &eventLog{streamID: streamID}
	l, err := wal.Open(filepath.Join(ds.path, streamID), wal.DefaultOptions)
	el.events = locker.New(l)
	return el, err
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
