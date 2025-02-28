package redis

import (
    "context"
    "time"
)

type TTLCache[Key comparable, Value any] struct {
    *Cache[Key, Value]
}

func NewTTL[K comparable, V any](opts *Options) *TTLCache[K, V] {
    if opts == nil {
        opts = DefaultOptions()
    }
    return &TTLCache[K, V]{
        Cache: New[K, V](opts),
    }
}

func (c *TTLCache[K, V]) Start(_ time.Duration) bool {
    // Redis handles TTL expiration automatically
    return true
}

func (c *TTLCache[K, V]) Stop() bool {
    return true
}

func (c *TTLCache[K, V]) SetTTL(ttl time.Duration, update bool) {
    c.Lock()
    defer c.Unlock()

    c.opts.DefaultTTL = ttl

    if update {
        ctx := context.Background()
        iter := c.pool.Client().Scan(ctx, 0, "*", 0).Iterator()
        for iter.Next(ctx) {
            key := iter.Val()
            c.pool.Client().Expire(ctx, key, ttl)
        }
    }
}
