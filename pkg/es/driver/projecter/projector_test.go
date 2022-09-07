package projecter_test

import (
	"context"
	"testing"

	"github.com/sour-is/ev/pkg/es/driver"
)

type mockDriver struct {
	onOpen     func()
	onEventLog func()
}

// EventLog implements driver.Driver
func (*mockDriver) EventLog(ctx context.Context, streamID string) (driver.EventLog, error) {
	panic("unimplemented")
}

// Open implements driver.Driver
func (*mockDriver) Open(ctx context.Context, dsn string) (driver.Driver, error) {
	panic("unimplemented")
}

var _ driver.Driver = (*mockDriver)(nil)

func TestProjecter(t *testing.T) {

}
