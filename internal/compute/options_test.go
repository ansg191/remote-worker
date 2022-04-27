package compute

import (
	"testing"
	"time"

	m "github.com/launchdarkly/go-test-helpers/v2/matchers"
)

func TestWithTickerInterval(t *testing.T) {
	tests := []struct {
		name     string
		interval time.Duration
	}{
		{"0", time.Duration(0)},
		{"seconds", 1 * time.Second},
		{"ms", 100 * time.Millisecond},
		{"hours", 2 * time.Hour},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := WithTickerInterval(test.interval)

			opt := new(ReadyOptions)

			f(opt)

			m.For(t, "val").Assert(opt.TickerInterval, m.Equal(test.interval))
		})
	}
}

func TestWithConnTimeout(t *testing.T) {
	tests := []struct {
		name     string
		interval time.Duration
	}{
		{"0", time.Duration(0)},
		{"seconds", 1 * time.Second},
		{"ms", 100 * time.Millisecond},
		{"hours", 2 * time.Hour},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := WithConnTimeout(test.interval)

			opt := new(ReadyOptions)

			f(opt)

			m.For(t, "val").Assert(opt.ConnTimeout, m.Equal(test.interval))
		})
	}
}
