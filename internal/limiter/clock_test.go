package limiter

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestClockNow(t *testing.T) {
	t.Parallel()

	clock := NewClock()
	before := time.Now()
	got := clock.Now()
	after := time.Now()

	if got.Before(before.Add(-10*time.Millisecond)) || got.After(after.Add(10*time.Millisecond)) {
		t.Fatalf("clock.Now out of expected range: %v", got)
	}
}

func TestClockSleep(t *testing.T) {
	t.Parallel()

	clock := NewClock()

	tests := []struct {
		name     string
		duration time.Duration
		cancel   bool
		wantErr  error
	}{
		{name: "zero duration", duration: 0, cancel: false, wantErr: nil},
		{name: "negative duration", duration: -time.Millisecond, cancel: false, wantErr: nil},
		{name: "positive duration", duration: 2 * time.Millisecond, cancel: false, wantErr: nil},
		{name: "context canceled", duration: 5 * time.Millisecond, cancel: true, wantErr: context.Canceled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			if tt.cancel {
				cancelCtx, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelCtx
			}

			err := clock.Sleep(ctx, tt.duration)
			if tt.wantErr == nil && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v; want %v", err, tt.wantErr)
			}
		})
	}
}
