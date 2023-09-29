// package driver defines interfaces to be used by driver implementations.
package driver

import (
	"context"

	"go.sour.is/ev/event"
	"go.sour.is/pkg/math"
)

type Driver interface {
	Open(ctx context.Context, dsn string) (Driver, error)
	EventLog(ctx context.Context, streamID string) (EventLog, error)
}

type EventLog interface {
	Read(ctx context.Context, after, count int64) (event.Events, error)
	ReadN(ctx context.Context, index ...uint64) (event.Events, error)
	Append(ctx context.Context, events event.Events, version uint64) (uint64, error)
	FirstIndex(context.Context) (uint64, error)
	LastIndex(context.Context) (uint64, error)
}

type EventLogWithTruncate interface {
	Truncate(context.Context, int64) error
}

type EventLogWithUpdate interface {
	LoadForUpdate(context.Context, event.Aggregate, func(context.Context, event.Aggregate) error) (uint64, error)
}

type Subscription interface {
	Recv(context.Context) <-chan bool
	Events(context.Context) (event.Events, error)
	Close(context.Context) error
}

type EventStream interface {
	Subscribe(ctx context.Context, streamID string, start int64) (Subscription, error)
	Send(ctx context.Context, streamID string, events event.Events) error
}

func GenerateStreamIDs(first, last uint64, after, count int64) ([]uint64, error) {
	// ---
	if first == 0 || last == 0 {
		return nil, nil
	}

	start, count := math.PagerBox(first, last, after, count)
	if count == 0 {
		return nil, nil
	}

	streamIDs := make([]uint64, math.Abs(count))
	for i := range streamIDs {
		streamIDs[i] = start

		if count > 0 {
			start += 1
		} else {
			start -= 1
		}
		if start < first || start > last {
			streamIDs = streamIDs[:i+1]
			break
		}
	}
	return streamIDs, nil
}
