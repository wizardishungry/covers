package covers_test

import (
	"jonwillia.ms/covers"
	"testing"
)

func TestOne(t *testing.T) {
	c := covers.Must(t)
	c.Tag("foobar").IsZero()
	c.Tag("foobar").Run(func(delta uint32) {
		t.Logf("foobar was %v", delta)
	})
	covers.One()
}
func TestTwo(t *testing.T) {
	c := covers.Must(t)
	c.Tag("foobar").IsNotZero()
	c.Tag("foobar").Run(func(delta uint32) {
		t.Logf("foobar was %v", delta)
	})
	covers.Two()
	covers.Two()

}
