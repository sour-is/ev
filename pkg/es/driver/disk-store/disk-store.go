package diskstore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tidwall/wal"

	"github.com/sour-is/ev/pkg/es"
	"github.com/sour-is/ev/pkg/es/driver"
	"github.com/sour-is/ev/pkg/es/event"
	"github.com/sour-is/ev/pkg/math"
)

type diskStore struct {
	path string
}

var _ driver.Driver = (*diskStore)(nil)

func Init(ctx context.Context) error {
	es.Register(ctx, "file", &diskStore{})
	return nil
}

func (diskStore) Open(dsn string) (driver.EventStore, error) {
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

func (es *diskStore) Save(ctx context.Context, agg event.Aggregate) (uint64, error) {
	l, err := es.readLog(agg.StreamID())
	if err != nil {
		return 0, err
	}

	var last uint64

	if last, err = l.w.LastIndex(); err != nil {
		return 0, err
	}

	if agg.StreamVersion() != last {
		return 0, fmt.Errorf("current version wrong %d != %d", agg.StreamVersion(), last)
	}

	events := agg.Events(true)

	var b []byte
	batch := &wal.Batch{}
	for _, e := range events {
		b, err = event.MarshalText(e)
		if err != nil {
			return 0, err
		}

		batch.Write(e.EventMeta().Position, b)
	}

	err = l.w.WriteBatch(batch)
	if err != nil {
		return 0, err
	}
	agg.Commit()

	return uint64(len(events)), nil
}
func (es *diskStore) Append(ctx context.Context, streamID string, events event.Events) (uint64, error) {
	event.SetStreamID(streamID, events...)

	l, err := es.readLog(streamID)
	if err != nil {
		return 0, err
	}

	var last uint64

	if last, err = l.w.LastIndex(); err != nil {
		return 0, err
	}

	var b []byte

	batch := &wal.Batch{}
	for i, e := range events {
		b, err = event.MarshalText(e)
		if err != nil {
			return 0, err
		}
		pos := last + uint64(i) + 1
		event.SetPosition(e, pos)

		batch.Write(pos, b)
	}

	err = l.w.WriteBatch(batch)
	if err != nil {
		return 0, err
	}
	return uint64(len(events)), nil
}
func (es *diskStore) Load(ctx context.Context, agg event.Aggregate) error {
	l, err := es.readLog(agg.StreamID())
	if err != nil {
		return err
	}

	var i, first, last uint64

	if first, err = l.w.FirstIndex(); err != nil {
		return err
	}
	if last, err = l.w.LastIndex(); err != nil {
		return err
	}

	if first == 0 || last == 0 {
		return nil
	}

	var b []byte
	events := make([]event.Event, last-i)

	for i = 0; first+i <= last; i++ {
		b, err = l.w.Read(first + i)
		if err != nil {
			return err
		}
		events[i], err = event.UnmarshalText(ctx, b, first+i)
		if err != nil {
			return err
		}
	}

	event.Append(agg, events...)

	return nil
}
func (es *diskStore) Read(ctx context.Context, streamID string, pos, count int64) (event.Events, error) {
	l, err := es.readLog(streamID)
	if err != nil {
		return nil, err
	}

	var first, last, start uint64

	if first, err = l.w.FirstIndex(); err != nil {
		return nil, err
	}
	if last, err = l.w.LastIndex(); err != nil {
		return nil, err
	}

	if first == 0 || last == 0 {
		return nil, nil
	}

	switch {
	case pos >= 0:
		start = first + uint64(pos)
		if pos == 0 && count < 0 {
			count = -count // if pos=0 assume forward count.
		}
	case pos < 0:
		start = uint64(int64(last) + pos + 1)
		if pos == -1 && count > 0 {
			count = -count // if pos=-1 assume backward count.
		}
	}

	events := make([]event.Event, math.Abs(count))
	for i := range events {
		var b []byte

		b, err = l.w.Read(start)
		if err != nil {
			return events, err
		}
		events[i], err = event.UnmarshalText(ctx, b, start)
		if err != nil {
			return events, err
		}

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

	event.SetStreamID(streamID, events...)

	return events, nil
}

func (es *diskStore) readLog(name string) (*eventLog, error) {
	return newEventLog(name, filepath.Join(es.path, name))
}

type eventLog struct {
	name string
	path string
	w    *wal.Log
}

func newEventLog(name, path string) (*eventLog, error) {
	var err error
	el := &eventLog{name: name, path: path}
	el.w, err = wal.Open(path, wal.DefaultOptions)
	return el, err
}
