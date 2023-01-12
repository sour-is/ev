package slice

import (
	"github.com/sour-is/ev/pkg/math"
)

// FilterType returns a subset that matches the type.
func FilterType[T any](in ...any) []T {
	lis := make([]T, 0, len(in))
	for _, u := range in {
		if t, ok := u.(T); ok {
			lis = append(lis, t)
		}
	}
	return lis
}

func FilterFn[T any](fn func(T) bool, in ...T) []T {
	lis := make([]T, 0, len(in))
	for _, t := range in {
		if fn(t) {
			lis = append(lis, t)
		}
	}
	return lis
}

// Find returns the first of type found. or false if not found.
func Find[T any](in ...any) (T, bool) {
	return First(FilterType[T](in...)...)
}

func FindFn[T any](fn func(T) bool, in ...T) (T, bool) {
	return First(FilterFn(fn, in...)...)
}

// First returns the first element in a slice.
func First[T any](in ...T) (T, bool) {
	if len(in) == 0 {
		var zero T
		return zero, false
	}
	return in[0], true
}

// Map applys func to each element s and returns results as slice.
func Map[T, U any](f func(T) U) func(...T) []U {
	return func(lis ...T) []U {
		r := make([]U, len(lis))
		for i, v := range lis {
			r[i] = f(v)
		}
		return r
	}
}

func Reduce[T, R any](r R, fn func(T, R, int) R) func(...T) R {
	return func(lis ...T) R {
		for i, t := range lis {
			r = fn(t, r, i)
		}
		return r
	}
}

type Pair[K comparable, V any] struct {
	Key   K
	Value V
}

func FromMap[K comparable, V any](m map[K]V) (keys []K, values []V) {
	if m == nil {
		return nil, nil
	}

	keys = make([]K, 0, len(m))
	values = make([]V, 0, len(m))

	for k := range m {
		keys = append(keys, k)
	}
	for _, k := range keys {
		values = append(values, m[k])
	}

	return keys, values
}

func ToMap[K comparable, V any](keys []K, values []V) (m map[K]V) {
	m = make(map[K]V, len(keys))

	for i := range keys {
		if len(values) < i {
			break
		}
		m[keys[i]] = values[i]
	}

	return m
}

func Zip[K comparable, V any](k []K, v []V) []Pair[K, V] {
	lis := make([]Pair[K, V], math.Max(len(k), len(v)))
	for i := range lis {
		if k != nil && len(k) > i {
			lis[i].Key = k[i]
		}

		if v != nil && len(v) > i {
			lis[i].Value = v[i]
		}
	}
	return lis
}

func Align[T any](k []T, v []T, less func(T, T) bool) []Pair[*T, *T] {
	lis := make([]Pair[*T, *T], 0, math.Max(len(k), len(v)))

	var j int

	for i := 0; i < len(k); {
		if j >= len(v) || less(k[i], v[j]) {
			lis = append(lis, Pair[*T, *T]{&k[i], nil})
			i++
			continue
		}

		if less(v[j], k[i]) {
			lis = append(lis, Pair[*T, *T]{nil, &v[j]})
			j++
			continue
		}

		lis = append(lis, Pair[*T, *T]{&k[i], &v[j]})
		i++
		j++
	}
	for ; j < len(v); j++ {
		lis = append(lis, Pair[*T, *T]{nil, &v[j]})
	}

	return lis

}
