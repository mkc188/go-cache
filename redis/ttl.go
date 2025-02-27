package redis

import (
    "context"
    "encoding/json"
    "fmt"
    "time"

    "github.com/go-redis/redis/v8"
)

type TTLCache[Key comparable, Value any] struct {
    *Cache[Key, Value]
    defaultTTL time.Duration
}

func NewTTL[K comparable, V any](opts *Options) *TTLCache[K, V] {
    if opts == nil {
        opts = DefaultOptions()
    }
    return &TTLCache[K, V]{
        Cache:      New[K, V](opts),
        defaultTTL: opts.DefaultTTL,
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

    c.defaultTTL = ttl

    if update {
        ctx := context.Background()
        iter := c.client.Scan(ctx, 0, "*", 0).Iterator()
        for iter.Next(ctx) {
            key := iter.Val()
            c.client.Expire(ctx, key, ttl)
        }
    }
}

func (c *TTLCache[K, V]) Add(key K, value V) bool {
    ctx := context.Background()
    data, err := json.Marshal(value)
    if err != nil {
        return false
    }

    success, err := c.client.SetNX(ctx, fmt.Sprint(key), data, c.defaultTTL).Result()
    return err == nil && success
}

func (c *TTLCache[K, V]) Set(key K, value V) {
    ctx := context.Background()
    data, err := json.Marshal(value)
    if err != nil {
        return
    }

    if err := c.client.Set(ctx, fmt.Sprint(key), data, c.defaultTTL).Err(); err != nil {
        // Log error here
    }
}
