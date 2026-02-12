package limiter

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeTimer struct {
	now      time.Time
	sleeps   []time.Duration
	sleepErr error
}

func (t *fakeTimer) Now() time.Time {
	return t.now
}

func (t *fakeTimer) Sleep(ctx context.Context, duration time.Duration) error {
	t.sleeps = append(t.sleeps, duration)
	if t.sleepErr != nil {
		return t.sleepErr
	}
	
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func TestNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		interval time.Duration
		wantNil  bool
	}{
		{name: "zero interval", interval: 0, wantNil: true},
		{name: "negative interval", interval: -time.Second, wantNil: true},
		{name: "positive interval", interval: time.Second, wantNil: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			limiter := New(tt.interval)
			if tt.wantNil && limiter != nil {
				t.Fatalf("expected nil limiter")
			}
			
			if !tt.wantNil && limiter == nil {
				t.Fatalf("expected non-nil limiter")
			}
		})
	}
}

func TestNewWithTimer(t *testing.T) {
	t.Parallel()

	clock := &fakeTimer{now: time.Unix(100, 0)}
	tests := []struct {
		name     string
		interval time.Duration
		timer    Timer
		wantNil  bool
	}{
		{name: "zero interval", interval: 0, timer: clock, wantNil: true},
		{name: "negative interval", interval: -time.Millisecond, timer: clock, wantNil: true},
		{name: "nil timer fallback", interval: time.Second, timer: nil, wantNil: false},
		{name: "custom timer", interval: time.Second, timer: clock, wantNil: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			limiter := NewWithTimer(tt.interval, tt.timer)
			if tt.wantNil && limiter != nil {
				t.Fatalf("expected nil limiter")
			}
			
			if !tt.wantNil && limiter == nil {
				t.Fatalf("expected non-nil limiter")
			}
		})
	}
}

func TestLimiterWaitNil(t *testing.T) {
	t.Parallel()

	var limiter *Limiter
	if err := limiter.Wait(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLimiterWaitFirstCallNoSleep(t *testing.T) {
	t.Parallel()

	clock := &fakeTimer{now: baseTime()}
	limiter := NewWithTimer(100*time.Millisecond, clock)

	if err := limiter.Wait(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	
	if len(clock.sleeps) != 0 {
		t.Fatalf("expected no sleep on first call, got %d", len(clock.sleeps))
	}
}

func TestLimiterWaitSecondCall(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		secondNow    time.Time
		wantSleep    []time.Duration
		expectWaitOK bool
	}{
		{
			name:         "sleeps until next interval",
			secondNow:    baseTime().Add(40 * time.Millisecond),
			wantSleep:    []time.Duration{60 * time.Millisecond},
			expectWaitOK: true,
		},
		{
			name:         "no sleep when already after interval",
			secondNow:    baseTime().Add(150 * time.Millisecond),
			wantSleep:    []time.Duration{},
			expectWaitOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			clock := &fakeTimer{now: baseTime()}
			limiter := NewWithTimer(100*time.Millisecond, clock)

			if err := limiter.Wait(context.Background()); err != nil {
				t.Fatalf("unexpected error on first call: %v", err)
			}

			clock.now = tt.secondNow
			
			err := limiter.Wait(context.Background())
			if tt.expectWaitOK && err != nil {
				t.Fatalf("unexpected error on second call: %v", err)
			}

			if len(clock.sleeps) != len(tt.wantSleep) {
				t.Fatalf("unexpected sleep call count: got %d want %d", len(clock.sleeps), len(tt.wantSleep))
			}
			
			for i := range tt.wantSleep {
				if clock.sleeps[i] != tt.wantSleep[i] {
					t.Fatalf("unexpected sleep duration[%d]: got %v want %v", i, clock.sleeps[i], tt.wantSleep[i])
				}
			}
		})
	}
}

func TestLimiterWaitReturnsSleepError(t *testing.T) {
	t.Parallel()

	errSleep := errors.New("sleep failed")
	clock := &fakeTimer{now: baseTime(), sleepErr: errSleep}
	limiter := NewWithTimer(100*time.Millisecond, clock)

	if err := limiter.Wait(context.Background()); err != nil {
		t.Fatalf("unexpected error on first call: %v", err)
	}

	clock.now = baseTime().Add(1 * time.Millisecond)
	
	err := limiter.Wait(context.Background())
	if !errors.Is(err, errSleep) {
		t.Fatalf("expected sleep error, got: %v", err)
	}
}

func baseTime() time.Time {
	return time.Date(2026, time.February, 12, 12, 0, 0, 0, time.UTC)
}
