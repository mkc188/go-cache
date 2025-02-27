package redis

import (
    "context"
    "encoding/json"
    "fmt"
    "sync"
    "time"

    "github.com/go-redis/redis/v8"
)

type Cache[Key comparable, Value any] struct {
    pool    *Pool
    opts    *Options
    evict   func(Key, Value)
    invalid func(Key, Value)
    sync.RWMutex
}

func New[K comparable, V any](opts *Options) *Cache[K, V] {
    if opts == nil {
        opts = DefaultOptions()
    }

    pool := NewPool(opts)

    return &Cache[K, V]{
        pool: pool,
        opts: opts,
    }
}

func (c *Cache[K, V]) Close() error {
    return c.pool.Close()
}

func (c *Cache[K, V]) SetEvictionCallback(hook func(K, V)) {
    c.Lock()
    c.evict = hook
    c.Unlock()
}

func (c *Cache[K, V]) SetInvalidateCallback(hook func(K, V)) {
    c.Lock()
    c.invalid = hook
    c.Unlock()
}

func (c *Cache[K, V]) Get(key K) (V, bool) {
    var value V
    ctx := context.Background()

    err := c.withRetry(ctx, func(ctx context.Context) error {
        data, err := c.pool.Client().Get(ctx, c.formatKey(key)).Bytes()
        if err != nil {
            if err == redis.Nil {
                return nil
            }
            return err
        }

        return json.Unmarshal(data, &value)
    })

    if err != nil {
        return value, false
    }

    return value, true
}

func (c *Cache[K, V]) Add(key K, value V) bool {
    ctx := context.Background()
    data, err := json.Marshal(value)
    if err != nil {
        return false
    }

    var success bool
    err = c.withRetry(ctx, func(ctx context.Context) error {
        result, err := c.pool.Client().SetNX(ctx, c.formatKey(key), data, c.opts.DefaultTTL).Result()
        if err != nil {
            return err
        }
        success = result
        return nil
    })

    return err == nil && success
}

func (c *Cache[K, V]) Set(key K, value V) {
    ctx := context.Background()
    data, err := json.Marshal(value)
    if err != nil {
        return
    }

    var oldValue V
    var hadOldValue bool

    err = c.withRetry(ctx, func(ctx context.Context) error {
        // Get old value for invalidation callback if needed
        if c.invalid != nil {
            oldData, err := c.pool.Client().Get(ctx, c.formatKey(key)).Bytes()
            if err == nil {
                if err := json.Unmarshal(oldData, &oldValue); err == nil {
                    hadOldValue = true
                }
            }
        }

        return c.pool.Client().Set(ctx, c.formatKey(key), data, c.opts.DefaultTTL).Err()
    })

    if err == nil && hadOldValue && c.invalid != nil {
        c.invalid(key, oldValue)
    }
}

func (c *Cache[K, V]) CAS(key K, old V, new V, cmp func(V, V) bool) bool {
    c.Lock()
    defer c.Unlock()

    current, exists := c.Get(key)
    if !exists {
        return false
    }

    if !cmp(old, current) {
        return false
    }

    ctx := context.Background()
    data, err := json.Marshal(new)
    if err != nil {
        return false
    }

    err = c.withRetry(ctx, func(ctx context.Context) error {
        return c.pool.Client().Set(ctx, c.formatKey(key), data, c.opts.DefaultTTL).Err()
    })

    if err == nil && c.invalid != nil {
        c.invalid(key, old)
    }

    return err == nil
}

func (c *Cache[K, V]) Swap(key K, swp V) V {
    c.Lock()
    defer c.Unlock()

    old, _ := c.Get(key)
    c.Set(key, swp)
    return old
}

func (c *Cache[K, V]) Has(key K) bool {
    ctx := context.Background()
    var exists bool

    err := c.withRetry(ctx, func(ctx context.Context) error {
        result, err := c.pool.Client().Exists(ctx, c.formatKey(key)).Result()
        if err != nil {
            return err
        }
        exists = result > 0
        return nil
    })

    return err == nil && exists
}

func (c *Cache[K, V]) Invalidate(key K) bool {
    ctx := context.Background()
    var success bool

    if oldVal, exists := c.Get(key); exists {
        err := c.withRetry(ctx, func(ctx context.Context) error {
            result, err := c.pool.Client().Del(ctx, c.formatKey(key)).Result()
            if err != nil {
                return err
            }
            success = result > 0
            return nil
        })

        if err == nil && success && c.invalid != nil {
            c.invalid(key, oldVal)
        }
    }

    return success
}

func (c *Cache[K, V]) InvalidateAll(keys ...K) bool {
    ctx := context.Background()
    redisKeys := make([]string, len(keys))
    oldValues := make(map[K]V)

    // Collect old values for invalidation callbacks
    if c.invalid != nil {
        for _, key := range keys {
            if oldVal, exists := c.Get(key); exists {
                oldValues[key] = oldVal
            }
        }
    }

    // Format keys for Redis
    for i, key := range keys {
        redisKeys[i] = c.formatKey(key)
    }

    var deleted int64
    err := c.withRetry(ctx, func(ctx context.Context) error {
        result, err := c.pool.Client().Del(ctx, redisKeys...).Result()
        if err != nil {
            return err
        }
        deleted = result
        return nil
    })

    // Call invalidation callbacks
    if err == nil && deleted > 0 && c.invalid != nil {
        for key, oldVal := range oldValues {
            c.invalid(key, oldVal)
        }
    }

    return err == nil && deleted > 0
}

func (c *Cache[K, V]) Clear() {
    ctx := context.Background()

    // If invalidation callback is set, we need to get all keys first
    if c.invalid != nil {
        var cursor uint64
        for {
            var keys []string
            var err error
            err = c.withRetry(ctx, func(ctx context.Context) error {
                keys, cursor, err = c.pool.Client().Scan(ctx, cursor, "*", 10).Result()
                return err
            })
            if err != nil {
                break
            }

            for _, key := range keys {
                if val, exists := c.Get(any(key).(K)); exists {
                    c.invalid(any(key).(K), val)
                }
            }

            if cursor == 0 {
                break
            }
        }
    }

    _ = c.withRetry(ctx, func(ctx context.Context) error {
        return c.pool.Client().FlushDB(ctx).Err()
    })
}

func (c *Cache[K, V]) Len() int {
    ctx := context.Background()
    var size int64

    err := c.withRetry(ctx, func(ctx context.Context) error {
        result, err := c.pool.Client().DBSize(ctx).Result()
        if err != nil {
            return err
        }
        size = result
        return nil
    })

    if err != nil {
        return 0
    }
    return int(size)
}

func (c *Cache[K, V]) Cap() int {
    return -1 // Redis has no fixed capacity
}

// Helper methods

func (c *Cache[K, V]) formatKey(key K) string {
    return fmt.Sprintf("%v", key)
}

// Transaction support

func (c *Cache[K, V]) WithTx(ctx context.Context, fn func(redis.Pipeliner) error) error {
    return c.withRetry(ctx, func(ctx context.Context) error {
        return c.pool.Client().Watch(ctx, fn)
    })
}

// Batch operations

func (c *Cache[K, V]) MGet(keys ...K) map[K]V {
    if len(keys) == 0 {
        return make(map[K]V)
    }

    ctx := context.Background()
    redisKeys := make([]string, len(keys))
    for i, key := range keys {
        redisKeys[i] = c.formatKey(key)
    }

    result := make(map[K]V)
    err := c.withRetry(ctx, func(ctx context.Context) error {
        pipe := c.pool.Client().Pipeline()
        for _, key := range redisKeys {
            pipe.Get(ctx, key)
        }

        cmds, err := pipe.Exec(ctx)
        if err != nil {
            return err
        }

        for i, cmd := range cmds {
            var value V
            data, err := cmd.(*redis.StringCmd).Bytes()
            if err == nil {
                if err := json.Unmarshal(data, &value); err == nil {
                    result[keys[i]] = value
                }
            }
        }
        return nil
    })

    if err != nil {
        return make(map[K]V)
    }

    return result
}

func (c *Cache[K, V]) MSet(items map[K]V) error {
    if len(items) == 0 {
        return nil
    }

    ctx := context.Background()
    return c.withRetry(ctx, func(ctx context.Context) error {
        pipe := c.pool.Client().Pipeline()

        for key, value := range items {
            data, err := json.Marshal(value)
            if err != nil {
                return err
            }
            pipe.Set(ctx, c.formatKey(key), data, c.opts.DefaultTTL)
        }

        _, err := pipe.Exec(ctx)
        return err
    })
}
