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
