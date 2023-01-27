package covers_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"jonwillia.ms/covers"
)

func TestOne(t *testing.T) {
	c := covers.Must(t)

	defer c.Tag("foobar", func(delta uint32) {
		if delta != 0 {
			t.Fatalf("foobar must be zero: %v", delta)
		}
	})

	defer c.Tag("foobar", func(delta uint32) { require.Zero(t, delta) })

	covers.One()
	c.Tag("foobar", func(delta uint32) { t.Log("foobar was", delta) })

}
func TestTwo(t *testing.T) {
	c := covers.Must(t)
	defer c.Tag("foobar", func(delta uint32) { require.NotZero(t, delta) })

	covers.Two()
	c.Tag("foobar", func(delta uint32) { t.Log("foobar was", delta) })
	covers.Two()
	c.Tag("foobar", func(delta uint32) { t.Log("foobar was", delta) })
}
