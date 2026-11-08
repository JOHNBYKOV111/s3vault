package resilience

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetry_SuccessOnThirdAttempt(t *testing.T) {
	attempts := 0
	fn := func() error {
		attempts++
		if attempts < 3 {
			return errors.New("fail")
		}
		return nil
	}

	ctx := context.Background()
	err := Retry(ctx, 5, 10*time.Millisecond, fn)
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestRetry_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Отменяем сразу

	fn := func() error { return errors.New("fail") }
	err := Retry(ctx, 5, 10*time.Millisecond, fn)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}
