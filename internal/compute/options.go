package compute

import "time"

type ReadyOptions struct {
	TickerInterval time.Duration
	ConnTimeout    time.Duration
}
