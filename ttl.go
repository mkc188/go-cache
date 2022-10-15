package cache

import (
	"sync"
	"time"

	"codeberg.org/gruf/go-maps"
)

// Entry represents an item in the cache, with it's currently calculated Expiry time.
type Entry[Key comparable, Value any] struct {
	Key    Key
	Value  Value
	Expiry time.Time
}

// TTLCache is the underlying Cache implementation, providing both the base Cache interface and unsafe access to underlying map to allow flexibility in building your own.
type TTLCache[Key comparable, Value any] struct {
	// TTL is the cache item TTL.
	TTL time.Duration

	// Evict is the hook that is called when an item is evicted from the cache, includes manual delete.
	Evict func(*Entry[Key, Value])

	// Invalid is the hook that is called when an item's data in the cache is invalidated.
	Invalid func(*Entry[Key, Value])

	// Cache is the underlying hashmap used for this cache.
	Cache maps.LRUMap[Key, *Entry[Key, Value]]

	// stop is the eviction routine cancel func.
	stop func()

	// pool is a memory pool of entry objects.
	pool []*Entry[Key, Value]

	// Embedded mutex.
	sync.Mutex
}

// Init will initialize this cache with given initial length, maximum capacity and item TTL.
func (c *TTLCache[K, V]) Init(len, cap int, ttl time.Duration) {
	if ttl <= 0 {
		// Default duration
		ttl = time.Second * 5
	}
	c.TTL = ttl
	c.SetEvictionCallback(nil)
	c.SetInvalidateCallback(nil)
	c.Cache.Init(len, cap)
}

// Start: implements cache.Cache's Start().
func (c *TTLCache[K, V]) Start(freq time.Duration) (ok bool) {
	// Nothing to start
	if freq <= 0 {
		return false
	}

	// Safely start
	c.Lock()

	if ok = c.stop == nil; ok {
		// Not yet running, schedule us
		c.stop = schedule(c.Sweep, freq)
	}

	// Done with lock
	c.Unlock()

	return
}

// Stop: implements cache.Cache's Stop().
func (c *TTLCache[K, V]) Stop() (ok bool) {
	// Safely stop
	c.Lock()

	if ok = c.stop != nil; ok {
		// We're running, cancel evicts
		c.stop()
		c.stop = nil
	}

	// Done with lock
	c.Unlock()

	return
}

// Sweep attempts to evict expired items (with callback!) from cache.
func (c *TTLCache[K, V]) Sweep(now time.Time) {
	var after int

	// Sweep within lock
	c.Lock()
	defer c.Unlock()

	// Sentinel value
	after = -1

	// The cache will be ordered by expiry date, we iterate until we reach the index of
	// the youngest item that hsa expired, as all succeeding items will also be expired.
	c.Cache.RangeIf(0, c.Cache.Len(), func(i int, _ K, item *Entry[K, V]) bool {
		if now.After(item.Expiry) {
			after = i

			// All older than this can be dropped
			return false
		}

		// Continue looping
		return true
	})

	// None yet expired
	if after == -1 {
		return
	}

	// Store list of evicted items for later callbacks
	evicts := make([]*Entry[K, V], 0, c.Cache.Len()-after-1)

	// Truncate all items after youngest eviction age.
	c.Cache.Truncate(cap(evicts), func(_ K, item *Entry[K, V]) {
		evicts = append(evicts, item)
	})

	// Pass each evicted to callback
	_ = c.Evict // nil check
	for _, item := range evicts {
		c.Evict(item)
		c.free(item)
	}
}

// SetEvictionCallback: implements cache.Cache's SetEvictionCallback().
func (c *TTLCache[K, V]) SetEvictionCallback(hook func(*Entry[K, V])) {
	// Ensure non-nil hook
	if hook == nil {
		hook = func(*Entry[K, V]) {}
	}

	// Update within lock
	c.Lock()
	defer c.Unlock()

	// Update hook
	c.Cache.Hook(func(_ K, item *Entry[K, V]) {
		hook(item)
	})
	c.Evict = hook
}

// SetInvalidateCallback: implements cache.Cache's SetInvalidateCallback().
func (c *TTLCache[K, V]) SetInvalidateCallback(hook func(*Entry[K, V])) {
	// Ensure non-nil hook
	if hook == nil {
		hook = func(*Entry[K, V]) {}
	}

	// Update within lock
	c.Lock()
	defer c.Unlock()

	// Update hook
	c.Invalid = hook
}

// SetTTL: implements cache.Cache's SetTTL().
func (c *TTLCache[K, V]) SetTTL(ttl time.Duration, update bool) {
	if ttl < 0 {
		panic("ttl must be greater than zero")
	}

	// Update within lock
	c.Lock()
	defer c.Unlock()

	// Set updated TTL
	diff := ttl - c.TTL
	c.TTL = ttl

	if update {
		// Update existing cache entries with new expiry time
		c.Cache.Range(0, c.Cache.Len(), func(i int, key K, item *Entry[K, V]) {
			item.Expiry = item.Expiry.Add(diff)
		})
	}
}

// Get: implements cache.Cache's Get().
func (c *TTLCache[K, V]) Get(key K) (V, bool) {
	// Read within lock
	c.Lock()
	defer c.Unlock()

	// Check for item in cache
	item, ok := c.Cache.Get(key)
	if !ok {
		var value V
		return value, false
	}

	// Update item expiry and return
	item.Expiry = time.Now().Add(c.TTL)
	return item.Value, true
}

// Add: implements cache.Cache's Add().
func (c *TTLCache[K, V]) Add(key K, value V) bool {
	// Write within lock
	c.Lock()
	defer c.Unlock()

	// If already cached, return
	if c.Cache.Has(key) {
		return false
	}

	// Alloc new item
	item := c.alloc()
	item.Key = key
	item.Value = value
	item.Expiry = time.Now().Add(c.TTL)

	// Place in the map
	c.Cache.Set(key, item)

	return true
}

// Set: implements cache.Cache's Set().
func (c *TTLCache[K, V]) Set(key K, value V) {
	// Write within lock
	c.Lock()
	defer c.Unlock()

	// Check if already exists
	item, ok := c.Cache.Get(key)

	if ok {
		// Invalidate existing
		c.Invalid(item)
	} else {
		// Allocate new item
		item = c.alloc()
		item.Key = key
		c.Cache.Set(key, item)
	}

	// Update the item value + expiry
	item.Expiry = time.Now().Add(c.TTL)
	item.Value = value
}

// CAS: implements cache.Cache's CAS().
func (c *TTLCache[K, V]) CAS(key K, old V, new V, cmp func(V, V) bool) bool {
	// CAS within lock
	c.Lock()
	defer c.Unlock()

	// Check for item in cache
	item, ok := c.Cache.Get(key)
	if !ok || !cmp(item.Value, old) {
		return false
	}

	// Invalidate item
	c.Invalid(item)

	// Update item + Expiry
	item.Value = new
	item.Expiry = time.Now().Add(c.TTL)

	return ok
}

// Swap: implements cache.Cache's Swap().
func (c *TTLCache[K, V]) Swap(key K, swp V) V {
	// Swap within lock
	c.Lock()
	defer c.Unlock()

	// Check for item in cache
	item, ok := c.Cache.Get(key)
	if !ok {
		var value V
		return value
	}

	// invalidate old
	c.Invalid(item)
	old := item.Value

	// update item + Expiry
	item.Value = swp
	item.Expiry = time.Now().Add(c.TTL)

	return old
}

// Has: implements cache.Cache's Has().
func (c *TTLCache[K, V]) Has(key K) bool {
	c.Lock()
	ok := c.Cache.Has(key)
	c.Unlock()
	return ok
}

// Invalidate: implements cache.Cache's Invalidate().
func (c *TTLCache[K, V]) Invalidate(key K) bool {
	// Delete within lock
	c.Lock()
	defer c.Unlock()

	// Check if we have item with key
	item, ok := c.Cache.Get(key)
	if !ok {
		return false
	}

	// Invalidate item
	c.Invalid(item)

	// Remove from cache map
	_ = c.Cache.Delete(key)

	// Return item to pool
	c.free(item)

	return true
}

// Clear: implements cache.Cache's Clear().
func (c *TTLCache[K, V]) Clear() {
	// Truncate within lock
	c.Lock()
	defer c.Unlock()

	// Store list of invalidated items for later callbacks
	deleted := make([]*Entry[K, V], 0, c.Cache.Len())

	// Truncate and store list of invalidated items
	c.Cache.Truncate(cap(deleted), func(_ K, item *Entry[K, V]) {
		deleted = append(deleted, item)
	})

	// Pass each invalidated to callback
	_ = c.Invalid // nil check
	for _, item := range deleted {
		c.Invalid(item)
		c.free(item)
	}
}

// Len: implements cache.Cache's Len().
func (c *TTLCache[K, V]) Len() int {
	c.Lock()
	l := c.Cache.Len()
	c.Unlock()
	return l
}

// Cap: implements cache.Cache's Cap().
func (c *TTLCache[K, V]) Cap() int {
	c.Lock()
	l := c.Cache.Cap()
	c.Unlock()
	return l
}

// alloc will acquire cache entry from pool, or allocate new.
func (c *TTLCache[K, V]) alloc() *Entry[K, V] {
	if len(c.pool) == 0 {
		return &Entry[K, V]{}
	}
	idx := len(c.pool) - 1
	e := c.pool[idx]
	c.pool = c.pool[:idx]
	return e
}

// free will reset entry fields and place back in pool.
func (c *TTLCache[K, V]) free(e *Entry[K, V]) {
	var (
		zk K
		zv V
	)
	e.Key = zk
	e.Value = zv
	e.Expiry = time.Time{}
	c.pool = append(c.pool, e)
}
