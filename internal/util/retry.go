package util

import (
	"context"
	"math"
	"time"
)

// Backoff computes an exponential delay for attempt n (0-based), capped
// at max. Used by health probes and by the supervisor's restart policy.
func Backoff(attempt int, base, max time.Duration) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	// 1<<attempt overflows fast; clamp the exponent before shifting.
	if attempt > 30 {
		return max
	}
	d := time.Duration(float64(base) * math.Pow(2, float64(attempt)))
	if d > max || d <= 0 {
		return max
	}
	return d
}

// Sleep is a context-aware sleep. It returns ctx.Err() if the context is
// cancelled first, so callers can propagate cancellation without a
// separate select at every call site.
func Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Retry calls fn up to attempts times, backing off between tries. It
// returns the last error if every attempt fails.
func Retry(ctx context.Context, attempts int, base, max time.Duration, fn func(ctx context.Context) error) error {
	var last error
	for i := 0; i < attempts; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if last = fn(ctx); last == nil {
			return nil
		}
		if i == attempts-1 {
			break
		}
		if err := Sleep(ctx, Backoff(i, base, max)); err != nil {
			return err
		}
	}
	return last
}
