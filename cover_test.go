package covers_test

import (
	"jonwillia.ms/covers"
	"testing"
)

func TestOne(t *testing.T) {
	c := covers.Must(t)
	c.NoHasKey("foobar")
	covers.One()
}
func TestTwo(t *testing.T) {
	c := covers.Must(t)
	c.HasKey("foobar")
	covers.Two()
	covers.Two()
}
