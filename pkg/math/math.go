package math

type signed interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64
}
type unsigned interface {
	~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr
}
type integer interface {
	signed | unsigned
}
type float interface {
	~float32 | ~float64
}
type ordered interface {
	integer | float | ~string
}

func Abs[T signed](i T) T {
	if i > 0 {
		return i
	}
	return -i
}
func Max[T ordered](i T, candidates ...T) T {
	for _, j := range candidates {
		if i < j {
			i = j
		}
	}
	return i
}
func Min[T ordered](i T, candidates ...T) T {
	for _, j := range candidates {
		if i > j {
			i = j
		}
	}
	return i
}
