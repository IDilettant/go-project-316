package limiter

import (
	"context"
	"sync"
	"time"
)

// Timer provides time for report generation and rate limiting.
type Timer interface {
	Now() time.Time
	Sleep(ctx context.Context, duration time.Duration) error
}

// Limiter enforces a minimum delay between requests.
type Limiter struct {
	mu       sync.Mutex
	interval time.Duration
	last     time.Time
	clock    Timer
}

// New creates a limiter using real time.
func New(interval time.Duration) *Limiter {
	if interval <= 0 {
		return nil
	}

	return &Limiter{
		interval: interval,
		clock:    Clock{},
	}
}

// NewWithTimer creates a limiter with a custom clock.
func NewWithTimer(interval time.Duration, clock Timer) *Limiter {
	if interval <= 0 {
		return nil
	}

	if clock == nil {
		clock = Clock{}
	}

	return &Limiter{
		interval: interval,
		clock:    clock,
	}
}

// Wait blocks until the next allowed request time or context cancellation.
func (l *Limiter) Wait(ctx context.Context) error {
	if l == nil {
		return nil
	}

	l.mu.Lock()
	now := l.clock.Now()
	if l.last.IsZero() {
		l.last = now
		l.mu.Unlock()
		
		return nil
	}

	next := l.last.Add(l.interval)
	if now.Before(next) {
		wait := next.Sub(now)
		l.last = next
		l.mu.Unlock()
		
		return l.clock.Sleep(ctx, wait)
	}

	l.last = now
	l.mu.Unlock()
	
	return nil
}
