package locker_test

import (
	"context"
	"errors"
	"testing"

	"github.com/matryer/is"

	"go.sour.is/ev/pkg/locker"
)

type config struct {
	Value   string
	Counter int
}

func TestLocker(t *testing.T) {
	is := is.New(t)

	value := locker.New(&config{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := value.Use(ctx, func(ctx context.Context, c *config) error {
		c.Value = "one"
		c.Counter++
		return nil
	})
	is.NoErr(err)

	c, err := value.Copy(context.Background())

	is.NoErr(err)
	is.Equal(c.Value, "one")
	is.Equal(c.Counter, 1)

	wait := make(chan struct{})

	go value.Use(ctx, func(ctx context.Context, c *config) error {
		c.Value = "two"
		c.Counter++
		close(wait)
		return nil
	})

	<-wait
	cancel()

	err = value.Use(ctx, func(ctx context.Context, c *config) error {
		c.Value = "three"
		c.Counter++
		return nil
	})
	is.True(err != nil)

	c, err = value.Copy(context.Background())

	is.NoErr(err)
	is.Equal(c.Value, "two")
	is.Equal(c.Counter, 2)
}

func TestNestedLocker(t *testing.T) {
	is := is.New(t)

	value := locker.New(&config{})
	other := locker.New(&config{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := value.Use(ctx, func(ctx context.Context, c *config) error {
		return value.Use(ctx, func(ctx context.Context, t *config) error {
			return nil
		})
	})
	is.True(errors.Is(err, locker.ErrNested))

	err = value.Use(ctx, func(ctx context.Context, c *config) error {
		return other.Use(ctx, func(ctx context.Context, t *config) error {
			return nil
		})
	})
	is.NoErr(err)

	err = value.Use(ctx, func(ctx context.Context, c *config) error {
		return other.Use(ctx, func(ctx context.Context, t *config) error {
			return value.Use(ctx, func(ctx context.Context, x *config) error {
				return nil
			})
		})
	})
	is.True(errors.Is(err, locker.ErrNested))
}
