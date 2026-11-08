package resilience

import (
	"context"
	"math/rand"
	"time"
)

// Retry выполняет fn до maxAttempts раз с экспоненциальной задержкой.
func Retry(ctx context.Context, maxAttempts int, baseDelay time.Duration, fn func() error) error {
	var err error
	for i := 0; i < maxAttempts; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if err = fn(); err == nil {
			return nil
		}

		// Экспоненциальная задержка с jitter
		delay := baseDelay * time.Duration(1<<i)
		jitter := time.Duration(rand.Int63n(int64(delay / 2)))
		select {
		case <-time.After(delay + jitter):
			// Продолжаем
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return err
}
