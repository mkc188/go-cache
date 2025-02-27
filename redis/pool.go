package redis

import (
    "context"
    "sync"
    "time"

    "github.com/go-redis/redis/v8"
)

type Pool struct {
    client  redis.UniversalClient
    opts    *Options
    health  *HealthChecker
    mu      sync.RWMutex
}

type HealthChecker struct {
    stopCh    chan struct{}
    interval  time.Duration
    threshold int
    failures  int
    mu        sync.RWMutex
}

func NewPool(opts *Options) *Pool {
    if opts == nil {
        opts = DefaultOptions()
    }

    client := redis.NewUniversalClient(&redis.UniversalOptions{
        Addrs:           opts.Addresses,
        Password:        opts.Password,
        DB:             opts.DB,
        PoolSize:        opts.PoolSize,
        MinIdleConns:    opts.MinIdleConns,
        MaxRetries:      opts.MaxRetries,
        MaxRetryBackoff: opts.RetryBackoff,
    })

    pool := &Pool{
        client: client,
        opts:   opts,
        health: &HealthChecker{
            stopCh:    make(chan struct{}),
            interval:  time.Second * 5,
            threshold: 3,
        },
    }

    pool.startHealthCheck()
    return pool
}

func (p *Pool) startHealthCheck() {
    go func() {
        ticker := time.NewTicker(p.health.interval)
        defer ticker.Stop()

        for {
            select {
            case <-p.health.stopCh:
                return
            case <-ticker.C:
                p.checkHealth()
            }
        }
    }()
}

func (p *Pool) checkHealth() {
    ctx, cancel := context.WithTimeout(context.Background(), time.Second)
    defer cancel()

    err := p.client.Ping(ctx).Err()

    p.health.mu.Lock()
    defer p.health.mu.Unlock()

    if err != nil {
        p.health.failures++
        if p.health.failures >= p.health.threshold {
            p.reconnect()
        }
    } else {
        p.health.failures = 0
    }
}

func (p *Pool) reconnect() {
    p.mu.Lock()
    defer p.mu.Unlock()

    oldClient := p.client
    p.client = redis.NewUniversalClient(&redis.UniversalOptions{
        Addrs:           p.opts.Addresses,
        Password:        p.opts.Password,
        DB:             p.opts.DB,
        PoolSize:        p.opts.PoolSize,
        MinIdleConns:    p.opts.MinIdleConns,
        MaxRetries:      p.opts.MaxRetries,
        MaxRetryBackoff: p.opts.RetryBackoff,
    })

    if oldClient != nil {
        oldClient.Close()
    }
}

func (p *Pool) Close() error {
    close(p.health.stopCh)
    return p.client.Close()
}

func (p *Pool) Client() redis.UniversalClient {
    p.mu.RLock()
    defer p.mu.RUnlock()
    return p.client
}
