package math_test

import (
	"testing"

	"github.com/matryer/is"
	"github.com/sour-is/ev/pkg/math"
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
