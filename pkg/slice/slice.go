package slice

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

// Find returns the first of type found. or false if not found.
func Find[T any](in ...any) (T, bool) {
	return First(FilterType[T](in...)...)
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
func Map[T, U any](s []T, f func(T) U) []U {
	r := make([]U, len(s))
	for i, v := range s {
		r[i] = f(v)
	}
	return r
}
