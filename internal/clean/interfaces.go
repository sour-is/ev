package clean

type GPD[K comparable, V any] interface {
	Get(...K) ([]V, error)
	Put(K, V) error
	Delete(K) error
}

type Edge[C, K comparable] struct {
	Key    K
	Kursor C
}
type Page[C, K comparable] struct {
	Edges Edge[C, K]
	Start C
	End   C
	Next  bool
	Prev  bool
}
type List[K, C comparable, V any] interface {
	First(n uint64, after C) ([]Page[C, K], error)
	Last(n uint64, before C) ([]Page[C, K], error)
}
type Emitter[T comparable, E any] interface {
	Emit(T, E) error
}
type Subscription[E any] interface {
	Recv() error
	Events() []E
	Close()
}
type Subscriber[T comparable, E any] interface {
	Subscribe(T, uint64) Subscription[E]
}
type Bus[T, K comparable, E any] interface {
	Emitter[T, E]
	Subscriber[T, E]
}
