package redis

import (
    "context"
    "time"

    "github.com/go-redis/redis/v8"
)

type RetryableFunc func(context.Context) error

func (c *Cache[K, V]) withRetry(ctx context.Context, fn RetryableFunc) error {
    var lastErr error
    for attempt := 0; attempt <= c.opts.MaxRetries; attempt++ {
        if attempt > 0 {
            select {
            case <-ctx.Done():
                return ctx.Err()
            case <-time.After(c.getBackoff(attempt)):
            }
        }

        err := fn(ctx)
        if err == nil {
            return nil
        }

        lastErr = err
        if !isRetryableError(err) {
            return err
        }
    }
    return lastErr
}

func (c *Cache[K, V]) getBackoff(attempt int) time.Duration {
    backoff := c.opts.RetryBackoff
    for i := 1; i < attempt; i++ {
        backoff *= 2
    }
    return backoff
}

func isRetryableError(err error) bool {
    if err == nil {
        return false
    }

    // Redis specific retryable errors
    switch err {
    case redis.ErrClosed,
         context.DeadlineExceeded,
         context.Canceled:
        return true
    }

    // Network errors
    if isNetworkError(err) {
        return true
    }

    return false
}

func isNetworkError(err error) bool {
    if err == nil {
        return false
    }

    // Check for network-related errors
    // This is a simplified check - in production you'd want more comprehensive error type checking
    return err.Error() == "connection refused" ||
        err.Error() == "connection reset by peer" ||
        err.Error() == "i/o timeout"
}
