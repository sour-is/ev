package locker

import (
	"context"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.sour.is/ev/internal/lg"
)

type Locked[T any] struct {
	state chan *T
}

// New creates a new locker for the given value.
func New[T any](initial *T) *Locked[T] {
	s := &Locked[T]{}
	s.state = make(chan *T, 1)
	s.state <- initial
	return s
}

type ctxKey struct{ name string }

// Use will call the function with the locked value
func (s *Locked[T]) Use(ctx context.Context, fn func(context.Context, *T) error) error {
	if s == nil {
		return fmt.Errorf("locker not initialized")
	}

	key := ctxKey{fmt.Sprintf("%p", s)}

	if value := ctx.Value(key); value != nil {
		return fmt.Errorf("%w: %T", ErrNested, s)
	}
	ctx = context.WithValue(ctx, key, key)

	ctx, span := lg.Span(ctx)
	defer span.End()

	var t T
	span.SetAttributes(
		attribute.String("typeOf", fmt.Sprintf("%T", t)),
	)

	if ctx.Err() != nil {
		return ctx.Err()
	}

	select {
	case state := <-s.state:
		defer func() { s.state <- state }()
		return fn(ctx, state)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Copy will return a shallow copy of the locked object.
func (s *Locked[T]) Copy(ctx context.Context) (T, error) {
	var t T

	err := s.Use(ctx, func(ctx context.Context, c *T) error {
		if c != nil {
			t = *c
		}
		return nil
	})

	return t, err
}

var ErrNested = errors.New("nested locker call")
