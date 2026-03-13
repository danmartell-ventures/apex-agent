package tunnel

import "time"

type backoff struct {
	min     time.Duration
	max     time.Duration
	current time.Duration
}

func newBackoff(min, max time.Duration) *backoff {
	return &backoff{min: min, max: max, current: min}
}

func (b *backoff) next() time.Duration {
	d := b.current
	b.current *= 2
	if b.current > b.max {
		b.current = b.max
	}
	return d
}

func (b *backoff) reset() {
	b.current = b.min
}
