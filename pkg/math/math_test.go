package math_test

import (
	"testing"

	"github.com/matryer/is"
	"go.sour.is/ev"
	"go.sour.is/ev/pkg/math"
)

func TestMath(t *testing.T) {
	is := is.New(t)

	is.Equal(5, math.Abs(-5))
	is.Equal(math.Abs(5), math.Abs(-5))

	is.Equal(10, math.Max(1, 2, 3, 4, 5, 6, 7, 8, 9, 10))
	is.Equal(1, math.Min(1, 2, 3, 4, 5, 6, 7, 8, 9, 10))

	is.Equal(1, math.Min(89, 71, 54, 48, 49, 1, 72, 88, 25, 69))
	is.Equal(89, math.Max(89, 71, 54, 48, 49, 1, 72, 88, 25, 69))

	is.Equal(0.9348207729, math.Max(
		0.3943310720,
		0.1090868377,
		0.9348207729,
		0.3525527584,
		0.4359833682,
		0.7958538081,
		0.1439352569,
		0.1547311967,
		0.6403818871,
		0.8618832818,
	))

	is.Equal(0.1090868377, math.Min(
		0.3943310720,
		0.1090868377,
		0.9348207729,
		0.3525527584,
		0.4359833682,
		0.7958538081,
		0.1439352569,
		0.1547311967,
		0.6403818871,
		0.8618832818,
	))

}

func TestPagerBox(t *testing.T) {
	is := is.New(t)

	tests := []struct {
		first uint64
		last  uint64
		pos   int64
		n     int64

		start uint64
		count int64
	}{
		{1, 10, 0, 10, 1, 10},
		{1, 10, 0, 11, 1, 10},
		{1, 5, 0, 10, 1, 5},
		{1, 10, 4, 10, 5, 6},
		{1, 10, 5, 10, 6, 5},
		{1, 10, 0, -10, 0, 0},
		{1, 10, 1, -1, 2, -1},
		{1, 10, 1, -10, 2, -1},
		{1, 10, -1, 1, 10, 1},
		{1, 10, -2, 10, 9, 2},
		{1, 10, -1, -1, 10, -1},
		{1, 10, -2, -10, 9, -9},
		{1, 10, 0, -10, 0, 0},
		{1, 10, 10, 10, 0, 0},
		{1, 10, 0, ev.AllEvents, 1, 10},
		{1, 10, -1, -ev.AllEvents, 10, -10},

		{5, 10, 0, 1, 5, 1},
	}

	for _, tt := range tests {
		start, count := math.PagerBox(tt.first, tt.last, tt.pos, tt.n)
		if count > 0 {
			t.Log(tt, "|", start, count, int64(start)+count-1)
		} else {
			t.Log(tt, "|", start, count, int64(start)+count+1)
		}

		is.Equal(start, tt.start)
		is.Equal(count, tt.count)
	}
}
