package set

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sour-is/ev/pkg/math"
)

type Set[T comparable] map[T]struct{}

func New[T comparable](items ...T) Set[T] {
	s := make(map[T]struct{}, len(items))
	for i := range items {
		s[items[i]] = struct{}{}
	}
	return s
}
func (s Set[T]) Has(v T) bool {
	_, ok := (s)[v]
	return ok
}
func (s Set[T]) Add(items ...T) Set[T] {
	for _, i := range items {
		s[i] = struct{}{}
	}
	return s
}
func (s Set[T]) Delete(items ...T) Set[T] {
	for _, i := range items {
		delete(s, i)
	}
	return s
}

func (s Set[T]) String() string {
	if s == nil {
		return "set(<nil>)"
	}
	lis := make([]string, 0, len(s))
	for k := range s {
		lis = append(lis, fmt.Sprint(k))
	}

	var b strings.Builder
	b.WriteString("set(")
	b.WriteString(strings.Join(lis, ","))
	b.WriteString(")")
	return b.String()
}

type ordered interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr |
		~float32 | ~float64
}

type BoundSet[T ordered] struct {
	min, max T
	s        Set[T]
}

func NewBoundSet[T ordered](min, max T, items ...T) *BoundSet[T] {
	b := &BoundSet[T]{
		min: min,
		max: max,
		s:   New[T](),
	}
	b.Add(items...)
	return b
}
func (l *BoundSet[T]) Add(items ...T) *BoundSet[T] {
	n := 0
	for i := range items {
		if items[i] >= l.min && items[i] <= l.max {
			items[n] = items[i]
			n++
		}
	}
	l.s.Add(items[:n]...)
	return l
}
func (l *BoundSet[T]) AddRange(min, max T) {
	min = math.Max(min, l.min)
	max = math.Min(max, l.max)
	var lis []T
	for ; min <= max; min++ {
		lis = append(lis, min)
	}
	l.s.Add(lis...)
}
func (l *BoundSet[T]) Delete(items ...T) *BoundSet[T] {
	n := 0
	for i := range items {
		if items[i] >= l.min && items[i] <= l.max {
			items[n] = items[i]
			n++
		}
	}
	l.s.Delete(items[:n]...)
	return l
}
func (l *BoundSet[T]) Has(v T) bool {
	return l.s.Has(v)
}
func (l *BoundSet[T]) String() string {
	lis := make([]string, len(l.s))
	n := 0
	for k := range l.s {
		lis[n] = fmt.Sprint(k)
		n++
	}
	sort.Strings(lis)

	var b strings.Builder
	b.WriteString("set(")
	b.WriteString(strings.Join(lis, ","))
	b.WriteString(")")
	return b.String()
}
