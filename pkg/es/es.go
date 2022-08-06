package es

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/sour-is/ev/pkg/es/driver"
	"github.com/sour-is/ev/pkg/locker"
)

type config struct {
	drivers map[string]driver.Driver
}

var (
	drivers = locker.New(&config{drivers: make(map[string]driver.Driver)})
)

func Register(ctx context.Context, name string, d driver.Driver) error {
	return drivers.Modify(ctx, func(c *config) error {
		if _, set := c.drivers[name]; set {
			return fmt.Errorf("driver %s already set", name)
		}
		c.drivers[name] = d
		return nil
	})
}

func Open(ctx context.Context, dsn string) (driver.EventStore, error) {
	name, _, ok := strings.Cut(dsn, ":")
	if !ok {
		return nil, fmt.Errorf("%w: no scheme", ErrNoDriver)
	}

	var d driver.Driver
	drivers.Modify(ctx,func(c *config) error {
		var ok bool
		d, ok = c.drivers[name]
		if !ok {
			return fmt.Errorf("%w: %s not registered", ErrNoDriver, name)
		}
		return nil
	})

	return d.Open(dsn)
}

var ErrNoDriver = errors.New("no driver")
