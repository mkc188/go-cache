package cache

import "time"

// Cache represents a TTL cache with customizable callbacks, it
// exists here to abstract away the "unsafe" methods in the case that
// you do not want your own implementation atop TTLCache{}.
type Cache[K, V comparable] interface {
	// Start will start the cache background eviction routine with given sweep frequency.
	// If already running or a freq <= 0 provided, this is a no-op. This will block until
	// the eviction routine has started
	Start(freq time.Duration) bool

	// Stop will stop cache background eviction routine. If not running this is a no-op. This
	// will block until the eviction routine has stopped
	Stop() bool

	// SetEvictionCallback sets the eviction callback to the provided hook
	SetEvictionCallback(hook Hook[K, V])

	// SetInvalidateCallback sets the invalidate callback to the provided hook
	SetInvalidateCallback(hook Hook[K, V])

	// SetTTL sets the cache item TTL. Update can be specified to force updates of existing items in
	// the cache, this will simply add the change in TTL to their current expiry time
	SetTTL(ttl time.Duration, update bool)

	// Get fetches the value with key from the cache, extending its TTL
	Get(key K) (value V, ok bool)

	// Put attempts to place the value at key in the cache, doing nothing if
	// a value with this key already exists. Returned bool is success state
	Put(key K, value V) bool

	// Set places the value at key in the cache. This will overwrite any
	// existing value, and call the update callback so. Existing values
	// will have their TTL extended upon update
	Set(key K, value V)

	// CAS will attempt to perform a CAS operation on 'key', using provided
	// comparison and swap values. Returned bool is success.
	CAS(key K, cmp, swp V) bool

	// Swap will attempt to perform a swap on 'key', replacing the value there
	// and returning the existing value. If no value exists for key, this will
	// set the value and return the zero value for V.
	Swap(key K, swp V) V

	// Has checks the cache for a value with key, this will not update TTL
	Has(key K) bool

	// Invalidate deletes a value from the cache, calling the invalidate callback
	Invalidate(key K) bool

	// Clear empties the cache, calling the invalidate callback
	Clear()

	// Size returns the current size of the cache
	Size() int
}

// New returns a new initialized Cache.
func New[K, V comparable]() Cache[K, V] {
	c := TTLCache[K, V]{}
	c.Init()
	return &c
}
