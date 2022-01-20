package cache

import (
	"context"
	"sync"
	"time"

	"codeberg.org/gruf/go-nowish"
	"codeberg.org/gruf/go-runners"
)

// clockPrecision is the precision of the cacheClock.
const clockPrecision = time.Millisecond * 100

var (
	// cacheClock is the cache-entry clock, used for TTL checking.
	cacheClock = nowish.Clock{}

	// clockOnce protects cacheClock from multiple starts
	clockOnce = sync.Once{}
)

// TTLCache is the underlying Cache implementation, providing both the base
// Cache interface and access to "unsafe" methods so that you may build your
// customized caches ontop of this structure.
type TTLCache[K, V comparable] struct {
	cache   map[K](*entry[V])
	evict   Hook[K, V]      // the evict hook is called when an item is evicted from the cache, includes manual delete
	invalid Hook[K, V]      // the invalidate hook is called when an item's data in the cache is invalidated
	ttl     time.Duration   // ttl is the item TTL
	svc     runners.Service // svc manages running of the cache eviction routine
	mu      sync.Mutex      // mu protects TTLCache for concurrent access
}

// Init performs Cache initialization, this MUST be called.
func (c *TTLCache[K, V]) Init() {
	// Initialize the cache itself
	c.cache = make(map[K](*entry[V]), 100)
	c.evict = emptyHook[K, V]
	c.invalid = emptyHook[K, V]
	c.ttl = time.Minute * 5
	c.Start(time.Second * 10)
	clockOnce.Do(func() { cacheClock.Start(clockPrecision) })
}

func (c *TTLCache[K, V]) Start(freq time.Duration) bool {
	// Nothing to start
	if freq < 1 {
		return false
	}

	// Check freq isn't too close to our unprecise cache clock
	if freq < 10*clockPrecision {
		panic("sweep freq too close to clock precision")
	}

	// Track state of starting
	done := make(chan struct{})
	started := false

	go func() {
		ran := c.svc.Run(func(ctx context.Context) {
			// Successfully started
			started = true
			close(done)

			// start routine
			c.run(ctx, freq)
		})

		// failed to start
		if !ran {
			close(done)
		}
	}()

	<-done
	return started
}

func (c *TTLCache[K, V]) Stop() bool {
	return c.svc.Stop()
}

func (c *TTLCache[K, V]) run(ctx context.Context, freq time.Duration) {
	t := time.NewTimer(freq)
	for {
		select {
		// we got stopped
		case <-ctx.Done():
			if !t.Stop() {
				<-t.C
			}
			return

		// next tick
		case <-t.C:
			c.sweep()
			t.Reset(freq)
		}
	}
}

// sweep attempts to evict expired items (with callback!) from cache.
func (c *TTLCache[K, V]) sweep() {
	// Lock and defer unlock (in case of hook panic)
	c.mu.Lock()
	defer c.mu.Unlock()

	// Fetch current time for TTL check
	now := cacheClock.Now()

	// Sweep the cache for old items!
	for key, item := range c.cache {
		if now.After(item.expiry) {
			c.evict(key, item.value)
			delete(c.cache, key)
		}
	}
}

// Lock locks the cache mutex.
func (c *TTLCache[K, V]) Lock() {
	c.mu.Lock()
}

// Unlock unlocks the cache mutex.
func (c *TTLCache[K, V]) Unlock() {
	c.mu.Unlock()
}

func (c *TTLCache[K, V]) SetEvictionCallback(hook Hook[K, V]) {
	// Ensure non-nil hook
	if hook == nil {
		hook = emptyHook[K, V]
	}

	// Safely set evict hook
	c.Lock()
	c.evict = hook
	c.Unlock()
}

func (c *TTLCache[K, V]) SetInvalidateCallback(hook Hook[K, V]) {
	// Ensure non-nil hook
	if hook == nil {
		hook = emptyHook[K, V]
	}

	// Safely set invalidate hook
	c.Lock()
	c.invalid = hook
	c.Unlock()
}

func (c *TTLCache[K, V]) SetTTL(ttl time.Duration, update bool) {
	if ttl < clockPrecision*10 && ttl > 0 {
		// A zero TTL means nothing expires,
		// but other small values we check to
		// ensure they won't be lost by our
		// unprecise cache clock
		panic("ttl too close to cache clock precision")
	}

	// Safely update TTL
	c.Lock()
	diff := ttl - c.ttl
	c.ttl = ttl

	if update {
		// Update existing cache entries
		for _, entry := range c.cache {
			entry.expiry.Add(diff)
		}
	}

	// We're done
	c.Unlock()
}

func (c *TTLCache[K, V]) Get(key K) (V, bool) {
	c.Lock()
	value, ok := c.GetUnsafe(key)
	c.Unlock()
	return value, ok
}

// GetUnsafe is the mutex-unprotected logic for Cache.Get().
func (c *TTLCache[K, V]) GetUnsafe(key K) (V, bool) {
	item, ok := c.cache[key]
	if !ok {
		var value V
		return value, false
	}
	item.expiry = cacheClock.Now().Add(c.ttl)
	return item.value, true
}

func (c *TTLCache[K, V]) Put(key K, value V) bool {
	c.Lock()
	success := c.PutUnsafe(key, value)
	c.Unlock()
	return success
}

// PutUnsafe is the mutex-unprotected logic for Cache.Put().
func (c *TTLCache[K, V]) PutUnsafe(key K, value V) bool {
	// If already cached, return
	if _, ok := c.cache[key]; ok {
		return false
	}

	// Create new cached item
	c.cache[key] = &entry[V]{
		value:  value,
		expiry: cacheClock.Now().Add(c.ttl),
	}

	return true
}

func (c *TTLCache[K, V]) Set(key K, value V) {
	c.Lock()
	defer c.Unlock() // defer in case of hook panic
	c.SetUnsafe(key, value)
}

// SetUnsafe is the mutex-unprotected logic for Cache.Set(), it calls externally-set functions.
func (c *TTLCache[K, V]) SetUnsafe(key K, value V) {
	item, ok := c.cache[key]
	if ok {
		// call invalidate hook
		c.invalid(key, item.value)
	} else {
		// alloc new item
		item = &entry[V]{}
		c.cache[key] = item
	}

	// Update the item + expiry
	item.value = value
	item.expiry = cacheClock.Now().Add(c.ttl)
}

func (c *TTLCache[K, V]) CAS(key K, cmp V, swp V) bool {
	c.Lock()
	ok := c.HasUnsafe(key)
	c.Unlock()
	return ok
}

// CASUnsafe is the mutex-unprotected logic for Cache.CAS().
func (c *TTLCache[K, V]) CASUnsafe(key K, cmp V, swp V) bool {
	// Check for item
	item, ok := c.cache[key]
	if !ok || item.value != cmp {
		return false
	}

	// Invalidate item
	c.invalid(key, item.value)

	// Update item + expiry
	item.value = swp
	item.expiry = cacheClock.Now().Add(c.ttl)

	return ok
}

func (c *TTLCache[K, V]) Swap(key K, swp V) V {
	c.Lock()
	old := c.SwapUnsafe(key, swp)
	c.Unlock()
	return old
}

// SwapUnsafe is the mutex-unprotected logic for Cache.Swap().
func (c *TTLCache[K, V]) SwapUnsafe(key K, swp V) V {
	// Check for item
	item, ok := c.cache[key]
	if !ok {
		var value V
		return value
	}

	// invalidate old item
	c.invalid(key, item.value)
	old := item.value

	// update item + expiry
	item.value = swp
	item.expiry = cacheClock.Now().Add(c.ttl)

	return old
}

func (c *TTLCache[K, V]) Has(key K) bool {
	c.Lock()
	ok := c.HasUnsafe(key)
	c.Unlock()
	return ok
}

// HasUnsafe is the mutex-unprotected logic for Cache.Has().
func (c *TTLCache[K, V]) HasUnsafe(key K) bool {
	_, ok := c.cache[key]
	return ok
}

func (c *TTLCache[K, V]) Invalidate(key K) bool {
	c.Lock()
	defer c.Unlock()
	return c.InvalidateUnsafe(key)
}

// InvalidateUnsafe is mutex-unprotected logic for Cache.Invalidate().
func (c *TTLCache[K, V]) InvalidateUnsafe(key K) bool {
	// Check if we have item with key
	item, ok := c.cache[key]
	if !ok {
		return false
	}

	// Call hook, remove from cache
	c.invalid(key, item.value)
	delete(c.cache, key)
	return true
}

func (c *TTLCache[K, V]) Clear() {
	c.Lock()
	defer c.Unlock()
	c.ClearUnsafe()
}

// ClearUnsafe is mutex-unprotected logic for Cache.Clean().
func (c *TTLCache[K, V]) ClearUnsafe() {
	for key, item := range c.cache {
		c.invalid(key, item.value)
		delete(c.cache, key)
	}
}

func (c *TTLCache[K, V]) Size() int {
	c.Lock()
	sz := c.SizeUnsafe()
	c.Unlock()
	return sz
}

// SizeUnsafe is mutex unprotected logic for Cache.Size().
func (c *TTLCache[K, V]) SizeUnsafe() int {
	return len(c.cache)
}

// entry represents an item in the cache, with
// it's currently calculated expiry time.
type entry[Value any] struct {
	value  Value
	expiry time.Time
}
