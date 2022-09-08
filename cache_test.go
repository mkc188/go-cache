package cache_test

import (
	"net/url"
	"testing"
	"time"
	"unsafe"

	"codeberg.org/gruf/go-cache/v3"
	"github.com/google/go-cmp/cmp"
)

var testEntries = map[string]interface{}{
	"key1":  "value1",
	"key2":  2,
	"a":     'a',
	"b":     '0',
	"c":     []string{"a", "b", "c"},
	"map":   map[string]string{"a": "a"},
	"iface": interface{}(nil),
	"weird": unsafe.Pointer(&cache.TTLCache[string, string]{}),
	"float": 2.4,
	"url":   url.URL{},
}

func TestCache(t *testing.T) {
	// Prepare cache
	c := cache.New[string, interface{}](10)
	c.SetTTL(time.Second*5, false)

	// Ensure we can start and stop it
	if !c.Start(time.Second * 10) {
		t.Fatal("failed to start cache eviction routine")
	}

	done := make(chan struct{})
	go func() {
		for {
			// Return if done
			select {
			case <-done:
				return
			default:
			}

			// Continually loop checking keys
			for key := range testEntries {
				c.Has(key)
			}
		}
	}()

	// Track callbacks set
	callbacks := map[string]interface{}{}
	c.SetInvalidateCallback(func(key string, value interface{}) {
		callbacks[key] = value
	})

	// Add all entries to cache
	for key, val := range testEntries {
		t.Logf("Cache.Put(%s, %v)", key, val)
		c.Put(key, val)
	}

	// Ensure all entries are expected
	for key, val := range testEntries {
		check, ok := c.Get(key)
		t.Logf("Cache.Get() => %s, %v", key, val)
		if !ok {
			t.Fatalf("key unexpectedly not found in cache: %s", key)
		} else if !cmp.Equal(val, check) {
			t.Fatalf("value not as expected for key in cache: %s", key)
		}
	}

	// Update entries via CAS to ensure callback
	for key, val := range testEntries {
		t.Logf("Cache.CAS(%s, %v, nil)", key, val)
		if !c.CAS(key, val, nil) && val != nil {
			t.Fatalf("CAS failed for: %s", key)
		} else if _, ok := callbacks[key]; !ok {
			t.Fatalf("invalidate callback not called for: %s", key)
		}
	}

	// Check values were updated
	for key := range testEntries {
		check, ok := c.Get(key)
		t.Logf("Cache.Get() => %s, %v", key, check)
		if !ok {
			t.Fatalf("key unexpectedly not found in cache: %s", key)
		} else if check != nil {
			t.Fatalf("value not as expected after update for key in cache: %s", key)
		}
	}

	// Clear callbacks, force invalidate and recheck
	callbacks = map[string]interface{}{}
	for key := range testEntries {
		t.Logf("Cache.Invalidate(%s)", key)
		c.Invalidate(key)
		if _, ok := callbacks[key]; !ok {
			t.Fatalf("invalidate callback unexpectedly not called for: %s", key)
		}
	}

	close(done) // stop the background loop
	t.Log("Sleeping to give time for cache sweeps")
	time.Sleep(time.Second * 15)

	// Checking cache is off expected size
	t.Logf("Checking cache is of expected size (0)")
	if sz := c.Size(); sz != 0 {
		t.Fatalf("unexpected cache size: %d", sz)
	}
}
