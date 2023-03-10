package test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"jonwillia.ms/covers"
)

func TestOne(t *testing.T) {
	ctrs := covers.Should(t)

	defer ctrs.Tag("foobar", func(delta uint32) {
		if delta != 0 {
			t.Fatalf("foobar must be zero: %v", delta)
		}
	})

	defer ctrs.Tag("foobar", func(delta uint32) { require.Zero(t, delta) })

	One()
	ctrs.Tag("foobar", func(delta uint32) { t.Log("foobar was", delta) })

}
func TestTwo(t *testing.T) {
	ctrs := covers.Should(t)
	defer ctrs.Tag("foobar", func(delta uint32) { require.NotZero(t, delta) })

	Two()
	ctrs.Tag("foobar", func(delta uint32) { t.Log("foobar was", delta) })
	Two()
	ctrs.Tag("foobar", func(delta uint32) { t.Log("foobar was", delta) })
}
