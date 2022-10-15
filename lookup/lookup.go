package lookup

import (
	"time"

	"codeberg.org/gruf/go-cache/v3/ttl"
	"github.com/cornelk/hashmap"
)

type Config[OK comparable, AK hashable, V any] struct {
	// RegisterLookups is called on init to register lookups within Cache's internal Map.
	RegisterLookups func(*Map[OK, AK])

	// AddLookups is called on each addition to the cache, to set any required additional key lookups for supplied item.
	AddLookups func(*Map[OK, AK], V)

	// DeleteLookups is called on each eviction/invalidation of an item in the cache, to remove any unused key lookups.
	DeleteLookups func(*Map[OK, AK], V)

	// TTL is the hash entry TTL duration.
	TTL time.Duration

	// Len, Cap are the cache initialization length, and maximum capacity.
	Len, Cap int
}

// Cache is a cache built on-top of TTLCache, providing multi-key lookups for items in the cache by means of additional lookup maps. These maps simply store additional keys => original key, with hook-ins to automatically call user supplied functions on adding an item, or on updating/deleting an item to keep the Map up-to-date.
type Cache[OK comparable, AK hashable, V any] struct {
	config Config[OK, AK, V]
	lookup Map[OK, AK]
	ttl.Cache[OK, V]
}

// New returns a new initialized Cache.
func New[OK comparable, AK hashable, V any](cfg Config[OK, AK, V]) *Cache[OK, AK, V] {
	c := new(Cache[OK, AK, V])
	c.Init(cfg)
	return c
}

// Init will initialize this cache with given configuration.
func (c *Cache[OK, AK, V]) Init(cfg Config[OK, AK, V]) {
	switch {
	case cfg.RegisterLookups == nil:
		panic("cache: nil lookups register function")
	case cfg.AddLookups == nil:
		panic("cache: nil lookups add function")
	case cfg.DeleteLookups == nil:
		panic("cache: nil delete lookups function")
	}
	c.config = cfg
	c.Cache.Init(cfg.Len, cfg.Cap, cfg.TTL)
	c.SetEvictionCallback(nil)
	c.SetInvalidateCallback(nil)
	c.lookup.lookup = hashmap.New[string, *hashmap.Map[AK, OK]]()
	c.config.RegisterLookups(&c.lookup)
}

// SetEvictioneCallback: implements cache.Cache's SetEvictionCallback().
func (c *Cache[OK, AK, V]) SetEvictionCallback(hook func(OK, V)) {
	if hook == nil {
		hook = func(o OK, v V) {}
	}
	c.Cache.SetEvictionCallback(func(item *ttl.Entry[OK, V]) {
		hook(item.Key, item.Value)
		c.config.DeleteLookups(&c.lookup, item.Value)
	})
}

// SetInvalidateCallback: implements cache.Cache's SetInvalidateCallback().
func (c *Cache[OK, AK, V]) SetInvalidateCallback(hook func(OK, V)) {
	if hook == nil {
		hook = func(o OK, v V) {}
	}
	c.Cache.SetInvalidateCallback(func(item *ttl.Entry[OK, V]) {
		hook(item.Key, item.Value)
		c.config.DeleteLookups(&c.lookup, item.Value)
	})
}

// GetBy fetches a cached value by supplied lookup identifier and key.
func (c *Cache[OK, AK, V]) GetBy(lookup string, key AK) (V, bool) {
	origKey, ok := c.lookup.Get(lookup, key)
	if !ok {
		var zero V
		return zero, false
	}
	return c.Cache.Get(origKey)
}

// Add: implements cache.Cache's Add().
func (c *Cache[OK, AK, V]) Add(key OK, value V) (ok bool) {
	if ok = c.Cache.Add(key, value); ok {
		c.config.AddLookups(&c.lookup, value)
	}
	return
}

// Set: implements cache.Cache's Set().
func (c *Cache[OK, AK, V]) Set(key OK, value V) {
	c.Cache.Set(key, value)
	c.config.AddLookups(&c.lookup, value)
}

// CASBy performs a CAS on value found at supplied lookup identifier and key.
func (c *Cache[OK, AK, V]) CASBy(lookup string, key AK, old, new V, cmp func(V, V) bool) bool {
	origKey, ok := c.lookup.Get(lookup, key)
	if !ok {
		return false
	}
	return c.Cache.CAS(origKey, old, new, cmp)
}

// SwapBy performs a swap on value found at supplied lookup identifier and key.
func (c *Cache[OK, AK, V]) SwapBy(lookup string, key AK, swp V) V {
	origKey, ok := c.lookup.Get(lookup, key)
	if !ok {
		var zero V
		return zero
	}
	return c.Cache.Swap(origKey, swp)
}

// HasBy checks if a value is cached under supplied lookup identifier and key.
func (c *Cache[OK, AK, V]) HasBy(lookup string, key AK) bool {
	return c.lookup.Has(lookup, key)
}

// InvalidateBy invalidates a value by supplied lookup identifier and key.
func (c *Cache[OK, AK, V]) InvalidateBy(lookup string, key AK) bool {
	origKey, ok := c.lookup.Get(lookup, key)
	if !ok {
		return false
	}
	c.Cache.Invalidate(origKey)
	return true
}

// Map is a structure that provides lookups for keys to primary keys under supplied lookup identifiers. This is essentially a wrapper around map[string](map[K1]K2).
type Map[OK comparable, AK hashable] struct {
	lookup *hashmap.Map[string, *hashmap.Map[AK, OK]]
}

// RegisterLookup registers a lookup identifier in the Map, note this can only be doing during the cfg.RegisterLookups() hook.
func (l *Map[OK, AK]) RegisterLookup(id string) {
	if _, ok := l.lookup.Get(id); ok {
		panic("cache: lookup mapping already exists for identifier")
	}
	l.lookup.Set(id, hashmap.New[AK, OK]())
}

// Get fetches an entry's primary key for lookup identifier and key.
func (l *Map[OK, AK]) Get(id string, key AK) (OK, bool) {
	keys, ok := l.lookup.Get(id)
	if !ok {
		var key OK
		return key, false
	}
	origKey, ok := keys.Get(key)
	return origKey, ok
}

// Set adds a lookup to the Map under supplied lookup identifier, linking supplied key to the supplied primary (original) key.
func (l *Map[OK, AK]) Set(id string, key AK, origKey OK) {
	keys, ok := l.lookup.Get(id)
	if !ok {
		panic("cache: unknown lookup identifier")
	}
	keys.Set(key, origKey)
}

// Has checks if there exists a lookup for supplied identifier and key.
func (l *Map[OK, AK]) Has(id string, key AK) bool {
	keys, ok := l.lookup.Get(id)
	if !ok {
		return false
	}
	_, ok = keys.Get(key)
	return ok
}

// Delete removes a lookup from Map with supplied identifier and key.
func (l *Map[OK, AK]) Delete(id string, key AK) {
	keys, ok := l.lookup.Get(id)
	if !ok {
		return
	}
	_ = keys.Del(key)
}

// hashable is pulled from hashmaps package as a generic parameter constraint.
type hashable interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr | ~float32 | ~float64 | ~string
}
