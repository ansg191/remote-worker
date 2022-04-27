package compute

import "time"

type ReadyOptions struct {
	TickerInterval time.Duration
	ConnTimeout    time.Duration
}

type ReadyOptionsFunc func(options *ReadyOptions)

func WithTickerInterval(interval time.Duration) ReadyOptionsFunc {
	return func(opts *ReadyOptions) {
		opts.TickerInterval = interval
	}
}

func WithConnTimeout(timeout time.Duration) ReadyOptionsFunc {
	return func(opts *ReadyOptions) {
		opts.ConnTimeout = timeout
	}
}
