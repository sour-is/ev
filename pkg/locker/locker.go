package locker

import "context"

type Locked[T any] struct {
	state chan *T
}

func New[T any](initial *T) *Locked[T] {
	s := &Locked[T]{}
	s.state = make(chan *T, 1)
	s.state <- initial
	return s
}

func (s *Locked[T]) Modify(ctx context.Context, fn func(*T) error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	select {
	case state := <-s.state:
		defer func() { s.state <- state }()
		return fn(state)
	case <-ctx.Done():
		return ctx.Err()
	}
}