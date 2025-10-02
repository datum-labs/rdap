package rdapclient

import "time"

// Backoff returns a sleep duration for attempt (1-based).
type Backoff func(attempt int) time.Duration

func ExponentialBackoff(start time.Duration, factor float64, max time.Duration) Backoff {
	if start <= 0 { start = 100 * time.Millisecond }
	if factor < 1.1 { factor = 1.5 }
	if max <= 0 { max = 2 * time.Second }
	return func(attempt int) time.Duration {
		d := float64(start)
		for i := 1; i < attempt; i++ { d *= factor }
		if d > float64(max) { d = float64(max) }
		return time.Duration(d)
	}
}
