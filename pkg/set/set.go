package set

import (
	"fmt"
	"strings"
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
