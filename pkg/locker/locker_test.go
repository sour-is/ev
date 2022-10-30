package locker_test

import (
	"context"
	"testing"

	"github.com/matryer/is"

	"github.com/sour-is/ev/pkg/locker"
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

	err := value.Modify(ctx, func(ctx context.Context, c *config) error {
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

	go value.Modify(ctx, func(ctx context.Context, c *config) error {
		c.Value = "two"
		c.Counter++
		close(wait)
		return nil
	})

	<-wait
	cancel()

	err = value.Modify(ctx, func(ctx context.Context, c *config) error {
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
