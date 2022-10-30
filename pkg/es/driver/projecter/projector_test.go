package projecter_test

import (
	"context"
	"testing"

	"github.com/matryer/is"
	"github.com/sour-is/ev/pkg/es"
	"github.com/sour-is/ev/pkg/es/driver"
	"github.com/sour-is/ev/pkg/es/driver/projecter"
	"github.com/sour-is/ev/pkg/es/event"
)

type mockDriver struct {
	onOpen     func(context.Context, string) (driver.Driver, error)
	onEventLog func(context.Context, string) (driver.EventLog, error)
}

// Open implements driver.Driver
func (m *mockDriver) Open(ctx context.Context, dsn string) (driver.Driver, error) {
	if m.onOpen != nil {
		return m.onOpen(ctx, dsn)
	}
	panic("unimplemented")
}

// EventLog implements driver.Driver
func (m *mockDriver) EventLog(ctx context.Context, streamID string) (driver.EventLog, error) {
	if m.onEventLog != nil {
		return m.onEventLog(ctx, streamID)
	}
	panic("unimplemented")
}

var _ driver.Driver = (*mockDriver)(nil)

type mockEventLog struct {
	onAppend     func(context.Context, event.Events, uint64) (uint64, error)
	onFirstIndex func(context.Context) (uint64, error)
	onLastIndex  func(context.Context) (uint64, error)
	onRead       func(context.Context, int64, int64) (event.Events, error)
	onReadN      func(context.Context, ...uint64) (event.Events, error)
}

// Append implements driver.EventLog
func (m *mockEventLog) Append(ctx context.Context, events event.Events, version uint64) (uint64, error) {
	if m.onAppend != nil {
		return m.onAppend(ctx, events, version)
	}
	panic("unimplemented")
}

// FirstIndex implements driver.EventLog
func (m *mockEventLog) FirstIndex(ctx context.Context) (uint64, error) {
	if m.onFirstIndex != nil {
		return m.onFirstIndex(ctx)
	}
	panic("unimplemented")
}

// LastIndex implements driver.EventLog
func (m *mockEventLog) LastIndex(ctx context.Context) (uint64, error) {
	if m.onLastIndex != nil {
		return m.onLastIndex(ctx)
	}
	panic("unimplemented")
}

// Read implements driver.EventLog
func (m *mockEventLog) Read(ctx context.Context, pos int64, count int64) (event.Events, error) {
	if m.onRead != nil {
		return m.onRead(ctx, pos, count)
	}

	panic("unimplemented")
}
func (m *mockEventLog) ReadN(ctx context.Context, index ...uint64) (event.Events, error) {
	if m.onReadN != nil {
		return m.onReadN(ctx, index...)
	}

	panic("unimplemented")
}

var _ driver.EventLog = (*mockEventLog)(nil)

func TestProjecter(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	var events []event.Event

	wait := make(chan struct{})

	mockEL := &mockEventLog{}
	mockEL.onRead = func(ctx context.Context, i1, i2 int64) (event.Events, error) {
		return event.NewEvents(), nil
	}
	mockEL.onAppend = func(ctx context.Context, e event.Events, u uint64) (uint64, error) {
		events = append(events, e...)
		if wait != nil && len(events) > 3 {
			close(wait)
		}
		return uint64(len(e)), nil
	}

	mock := &mockDriver{}
	mock.onOpen = func(ctx context.Context, s string) (driver.Driver, error) {
		return mock, nil
	}
	mock.onEventLog = func(ctx context.Context, s string) (driver.EventLog, error) {
		return mockEL, nil
	}

	es.Init(ctx)
	es.Register(ctx, "mock", mock)

	es, err := es.Open(
		ctx,
		"mock:",
		projecter.New(ctx, projecter.DefaultProjection),
	)

	is.NoErr(err)

	_, err = es.Read(ctx, "test", 0, 1)

	is.NoErr(err)

	_, err = es.Append(ctx, "test", event.NewEvents(event.NilEvent))
	is.NoErr(err)

	<-wait

	is.Equal(len(events), 4)

}
