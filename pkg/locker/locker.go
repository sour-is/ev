package locker

import (
	"context"
	"log"
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

// Modify will call the function with the locked value
func (s *Locked[T]) Modify(ctx context.Context, fn func(*T) error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	select {
	case state := <-s.state:
		defer func() { s.state <- state }()
		log.Printf("locker %T to %p", state, fn)
		defer log.Printf("locker %T from %p", state, fn)

		return fn(state)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Copy will return a shallow copy of the locked object.
func (s *Locked[T]) Copy(ctx context.Context) (T, error) {
	var t T

	err := s.Modify(ctx, func(c *T) error {
		if c != nil {
			t = *c
		}
		return nil
	})

	return t, err
}
