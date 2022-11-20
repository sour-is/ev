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

func PagerBox(first, last uint64, pos, count int64) (uint64, int64) {
	var start uint64

	if pos >= 0 {
		if int64(first) > pos {
			start = first
		} else {
			start = uint64(pos) + 1
		}
	} else {
		start = uint64(int64(last) + pos + 1)
	}

	switch {
	case count > 0:
		count = Min(count, int64(last-start)+1)

	case pos >= 0 && count < 0:
		count = Max(count, int64(first-start))

	case pos < 0 && count < 0:
		count = Max(count, int64(first-start)-1)
	}

	if count == 0 || (start < first && count <= 0) || (start > last && count >= 0) {
		return 0, 0
	}

	return start, count
}
