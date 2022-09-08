package fancycache

import (
	"reflect"
	"sync"
	"time"

	"codeberg.org/gruf/go-cache/v3"
)

// Cache ...
type Cache[Value any] struct {
	cache cache.TTLCache[string, *entry[Value]]
	keys  structKeys
	pool  sync.Pool
}

// New ...
func New[Value any](sz int, lookups []string) *Cache[Value] {
	var z Value

	// Determine generic type info
	t := reflect.TypeOf(z)

	// Iteratively deref pointer type
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	// Ensure that this is a struct type
	if t.Kind() != reflect.Struct {
		panic("generic parameter type must be struct (or ptr to)")
	}

	// Preallocate a slice of keyed fields info
	keys := make([]keyFields, len(lookups))

	for i, lookup := range lookups {
		// Generate keyed field info for lookup
		keys[i] = keyFields{prefix: lookup}
		keys[i].populate(t)
	}

	// Create and initialize
	c := &Cache[Value]{keys: keys}
	c.SetEvictionCallback(nil)
	c.SetInvalidateCallback(nil)
	c.cache.Cache = make(map[string]*cache.Entry[*entry[Value]], sz)
	c.cache.TTL = time.Minute * 5
	return c
}

// Start will start the cache background eviction routine with given sweep frequency. If already
// running or a freq <= 0 provided, this is a no-op. This will block until eviction routine started.
func (c *Cache[Value]) Start(freq time.Duration) bool {
	return c.cache.Start(freq)
}

// Stop will stop cache background eviction routine. If not running this
// is a no-op. This will block until the eviction routine has stopped.
func (c *Cache[Value]) Stop() bool {
	return c.cache.Stop()
}

// SetTTL sets the cache item TTL. Update can be specified to force updates of existing items
// in the cache, this will simply add the change in TTL to their current expiry time.
func (c *Cache[Value]) SetTTL(ttl time.Duration, update bool) {
	c.cache.SetTTL(ttl, update)
}

// SetEvictionCallback sets the eviction callback to the provided hook.
func (c *Cache[Value]) SetEvictionCallback(hook func(Value)) {
	if hook == nil {
		// Ensure non-nil hook
		hook = func(Value) {}
	}
	c.cache.SetEvictionCallback(func(key string, value *entry[Value]) {
		for i := range value.keys {
			// This is "us", already deleted.
			if value.keys[i].value == key {
				continue
			}

			// Manually delete this extra cache key
			delete(c.cache.Cache, value.keys[i].value)
		}

		// Call user hook
		hook(value.value)
	})
}

// SetInvalidateCallback sets the invalidate callback to the provided hook.
func (c *Cache[Value]) SetInvalidateCallback(hook func(Value)) {
	if hook == nil {
		// Ensure non-nil hook
		hook = func(Value) {}
	}
	c.cache.SetInvalidateCallback(func(key string, value *entry[Value]) {
		for i := range value.keys {
			// This is "us", already deleted.
			if value.keys[i].value == key {
				continue
			}

			// Manually delete this extra cache key
			delete(c.cache.Cache, value.keys[i].value)
		}

		// Call user hook
		hook(value.value)
	})
}

// Get ...
func (c *Cache[Value]) Get(lookup string, keyParts ...any) (Value, bool) {
	// Generate cache key string
	ckey := genkey(lookup, keyParts...)

	// Look for struct value in cache
	val, ok := c.cache.Get(ckey)

	if !ok {
		var zero Value
		return zero, false
	}

	return val.value, true
}

// Put ...
func (c *Cache[Value]) Put(value Value) bool {
	// Acquire cache lock
	c.cache.Lock()
	defer c.cache.Unlock()

	// Prepare cached value
	val := entry[Value]{
		keys:  c.keys.generate(value),
		value: value,
	}

	// Check for overlapy with any keys, as an
	// overlap will cause say one but not all of
	// an item's keys to produce unexpected results.
	for _, key := range val.keys {
		if c.cache.HasUnsafe(key.value) {
			return false
		}
	}

	// Store this result under all keys
	for _, key := range val.keys {
		c.cache.SetUnsafe(key.value, &val)
	}

	return true
}

// Has ...
func (c *Cache[Value]) Has(lookup string, keyParts ...any) bool {
	// Generate cache key string
	ckey := genkey(lookup, keyParts...)

	// Check for struct value
	return c.cache.Has(ckey)
}

// Invalidate ...
func (c *Cache[Value]) Invalidate(lookup string, keyParts ...any) {
	// Generate cache key string
	ckey := genkey(lookup, keyParts...)

	// Invalidate this key from cache
	c.cache.Invalidate(ckey)
}

// Clear empties the cache, calling the invalidate callback
func (cache *Cache[Value]) Clear() {
	cache.cache.Clear()
}

// entry ...
type entry[Value any] struct {
	keys  []cacheKey
	value Value
}
