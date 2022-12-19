package set_test

import (
	"strings"
	"testing"

	"github.com/matryer/is"
	"github.com/sour-is/ev/pkg/set"
)

func TestStringSet(t *testing.T) {
	is := is.New(t)

	s := set.New(strings.Fields("one two  three")...)

	is.True(s.Has("one"))
	is.True(s.Has("two"))
	is.True(s.Has("three"))
	is.True(!s.Has("four"))

	is.Equal(set.New("one").String(), "set(one)")

	var n set.Set[string]
	is.Equal(n.String(), "set(<nil>)")
}

func TestBoundSet(t *testing.T) {
	is := is.New(t)

	s := set.NewBoundSet(1, 100, 1, 2, 3, 100, 1001)

	is.True(s.Has(1))
	is.True(s.Has(2))
	is.True(s.Has(3))
	is.True(!s.Has(1001))

	is.Equal(set.NewBoundSet(1, 100, 1).String(), "set(1)")

}
