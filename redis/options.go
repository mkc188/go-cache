package redis

import (
    "time"
)

type Options struct {
    // Redis connection options
    Addresses []string // Redis addresses (for cluster support)
    Password  string
    DB        int

    // Connection pool options
    PoolSize     int
    MinIdleConns int
    MaxRetries   int
    RetryBackoff time.Duration

    // Cache options
    DefaultTTL time.Duration
}

func DefaultOptions() *Options {
    return &Options{
        Addresses:    []string{"localhost:6379"},
        PoolSize:     10,
        MinIdleConns: 2,
        MaxRetries:   3,
        RetryBackoff: time.Millisecond * 100,
        DefaultTTL:   time.Hour,
    }
}
