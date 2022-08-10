package driver

import (
	"context"

	"github.com/sour-is/ev/pkg/es/event"
)

type Driver interface {
	Open(ctx context.Context, dsn string) (Driver, error)
	EventLog(ctx context.Context, streamID string) (EventLog, error)
}

type EventLog interface {
	Read(ctx context.Context, pos, count int64) (event.Events, error)
	Append(ctx context.Context, events event.Events, version uint64) (uint64, error)
	FirstIndex(ctx context.Context) (uint64, error)
	LastIndex(ctx context.Context) (uint64, error)
}

type Subscription interface {
	Recv(context.Context) bool
	Events(context.Context) (event.Events, error)
	Close(context.Context) error
}

type EventStream interface {
	Subscribe(ctx context.Context, streamID string, start int64) (Subscription, error)
	Send(ctx context.Context, streamID string, events event.Events) error
}
