package limiter 

import (
	"context"
	"time"
)

type Clock struct{}

func NewClock() Clock {
	return Clock{}
}

func (Clock) Now() time.Time {
	return time.Now()
}

func (Clock) Sleep(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}

	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
