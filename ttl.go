package cache

import (
	"sync"
	"time"
)

// TTLCache is the underlying Cache implementation, providing both the base
// Cache interface and access to "unsafe" methods so that you may build your
// customized caches ontop of this structure.
type TTLCache[Key comparable, Value any] struct {
	// TTL is the cache item TTL.
	TTL time.Duration

	// Evict is the hook that is called when an item is
	// evicted from the cache, includes manual delete.
	Evict func(Key, Value)

	// Invalid is the hook that is called when an item's
	// data in the cache is invalidated.
	Invalid func(Key, Value)

	// Cache is the underlying hashmap used for this cache.
	Cache map[Key](*Entry[Value])

	stop func()     // stop is the cancel function for the scheduled eviction routine
	mu   sync.Mutex // mu protects TTLCache for concurrent access
}

func (c *TTLCache[K, V]) Start(freq time.Duration) (ok bool) {
	// Nothing to start
	if freq <= 0 {
		return false
	}

	// Safely start
	c.mu.Lock()

	if ok = c.stop == nil; ok {
		// Not yet running, schedule us
		c.stop = schedule(c.sweep, freq)
	}

	// Done with lock
	c.mu.Unlock()

	return
}

func (c *TTLCache[K, V]) Stop() (ok bool) {
	// Safely stop
	c.mu.Lock()

	if ok = c.stop != nil; ok {
		// We're running, cancel evicts
		c.stop()
		c.stop = nil
	}

	// Done with lock
	c.mu.Unlock()

	return
}

// sweep attempts to evict expired items (with callback!) from cache.
func (c *TTLCache[K, V]) sweep(now time.Time) {
	// Lock and defer unlock (in case of hook panic)
	c.mu.Lock()
	defer c.mu.Unlock()

	// Sweep the cache for old items!
	for key, item := range c.Cache {
		if now.After(item.Expiry) {
			c.Evict(key, item.Value)
			delete(c.Cache, key)
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

func (c *TTLCache[K, V]) SetEvictionCallback(hook func(K, V)) {
	// Ensure non-nil hook
	if hook == nil {
		hook = func(K, V) {}
	}

	// Safely set evict hook
	c.mu.Lock()
	c.Evict = hook
	c.mu.Unlock()
}

func (c *TTLCache[K, V]) SetInvalidateCallback(hook func(K, V)) {
	// Ensure non-nil hook
	if hook == nil {
		hook = func(K, V) {}
	}

	// Safely set invalidate hook
	c.mu.Lock()
	c.Invalid = hook
	c.mu.Unlock()
}

func (c *TTLCache[K, V]) SetTTL(ttl time.Duration, update bool) {
	// Safely update TTL
	c.mu.Lock()
	diff := ttl - c.TTL
	c.TTL = ttl

	if update {
		// Update existing cache entries
		for _, Entry := range c.Cache {
			Entry.Expiry.Add(diff)
		}
	}

	// We're done
	c.mu.Unlock()
}

func (c *TTLCache[K, V]) Get(key K) (V, bool) {
	c.mu.Lock()
	value, ok := c.GetUnsafe(key)
	c.mu.Unlock()
	return value, ok
}

// GetUnsafe is the mutex-unprotected logic for Cache.Get().
func (c *TTLCache[K, V]) GetUnsafe(key K) (V, bool) {
	item, ok := c.Cache[key]
	if !ok {
		var value V
		return value, false
	}
	item.Expiry = time.Now().Add(c.TTL)
	return item.Value, true
}

func (c *TTLCache[K, V]) Put(key K, value V) bool {
	c.mu.Lock()
	success := c.PutUnsafe(key, value)
	c.mu.Unlock()
	return success
}

// PutUnsafe is the mutex-unprotected logic for Cache.Put().
func (c *TTLCache[K, V]) PutUnsafe(key K, value V) bool {
	// If already cached, return
	if _, ok := c.Cache[key]; ok {
		return false
	}

	// Create new cached item
	c.Cache[key] = &Entry[V]{
		Value:  value,
		Expiry: time.Now().Add(c.TTL),
	}

	return true
}

func (c *TTLCache[K, V]) Set(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock() // defer in case of hook panic
	c.SetUnsafe(key, value)
}

// SetUnsafe is the mutex-unprotected logic for Cache.Set(), it calls externally-set functions.
func (c *TTLCache[K, V]) SetUnsafe(key K, value V) {
	item, ok := c.Cache[key]
	if ok {
		// call invalidate hook
		c.Invalid(key, item.Value)
	} else {
		// alloc new item
		item = &Entry[V]{}
		c.Cache[key] = item
	}

	// Update the item + Expiry
	item.Value = value
	item.Expiry = time.Now().Add(c.TTL)
}

func (c *TTLCache[K, V]) CAS(key K, cmp V, swp V) bool {
	c.mu.Lock()
	ok := c.CASUnsafe(key, cmp, swp)
	c.mu.Unlock()
	return ok
}

// CASUnsafe is the mutex-unprotected logic for Cache.CAS().
func (c *TTLCache[K, V]) CASUnsafe(key K, cmp V, swp V) bool {
	// Check for item
	item, ok := c.Cache[key]
	if !ok || !Compare(item.Value, cmp) {
		return false
	}

	// Invalidate item
	c.Invalid(key, item.Value)

	// Update item + Expiry
	item.Value = swp
	item.Expiry = time.Now().Add(c.TTL)

	return ok
}

func (c *TTLCache[K, V]) Swap(key K, swp V) V {
	c.mu.Lock()
	old := c.SwapUnsafe(key, swp)
	c.mu.Unlock()
	return old
}

// SwapUnsafe is the mutex-unprotected logic for Cache.Swap().
func (c *TTLCache[K, V]) SwapUnsafe(key K, swp V) V {
	// Check for item
	item, ok := c.Cache[key]
	if !ok {
		var value V
		return value
	}

	// invalidate old item
	c.Invalid(key, item.Value)
	old := item.Value

	// update item + Expiry
	item.Value = swp
	item.Expiry = time.Now().Add(c.TTL)

	return old
}

func (c *TTLCache[K, V]) Has(key K) bool {
	c.mu.Lock()
	ok := c.HasUnsafe(key)
	c.mu.Unlock()
	return ok
}

// HasUnsafe is the mutex-unprotected logic for Cache.Has().
func (c *TTLCache[K, V]) HasUnsafe(key K) bool {
	_, ok := c.Cache[key]
	return ok
}

func (c *TTLCache[K, V]) Invalidate(key K) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.InvalidateUnsafe(key)
}

// InvalidateUnsafe is mutex-unprotected logic for Cache.Invalidate().
func (c *TTLCache[K, V]) InvalidateUnsafe(key K) bool {
	// Check if we have item with key
	item, ok := c.Cache[key]
	if !ok {
		return false
	}

	// Call hook, remove from cache
	c.Invalid(key, item.Value)
	delete(c.Cache, key)
	return true
}

func (c *TTLCache[K, V]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ClearUnsafe()
}

// ClearUnsafe is mutex-unprotected logic for Cache.Clean().
func (c *TTLCache[K, V]) ClearUnsafe() {
	for key, item := range c.Cache {
		c.Invalid(key, item.Value)
		delete(c.Cache, key)
	}
}

func (c *TTLCache[K, V]) Size() int {
	c.mu.Lock()
	sz := c.SizeUnsafe()
	c.mu.Unlock()
	return sz
}

// SizeUnsafe is mutex unprotected logic for Cache.Size().
func (c *TTLCache[K, V]) SizeUnsafe() int {
	return len(c.Cache)
}

// Entry represents an item in the cache, with
// it's currently calculated Expiry time.
type Entry[Value any] struct {
	Value  Value
	Expiry time.Time
}
