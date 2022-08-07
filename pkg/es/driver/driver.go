package driver

import (
	"context"

	"github.com/sour-is/ev/pkg/es/event"
)

type Driver interface {
	Open(string) (EventStore, error)
}

type EventStore interface {
	Save(ctx context.Context, agg event.Aggregate) (uint64, error)
	Load(ctx context.Context, agg event.Aggregate) error
	Read(ctx context.Context, streamID string, pos, count int64) (event.Events, error)
	Append(ctx context.Context, streamID string, events event.Events) (uint64, error)
	FirstIndex(ctx context.Context, streamID string) (uint64, error)
	LastIndex(ctx context.Context, streamID string) (uint64, error)
}
