package ttl_test

import (
	"net/url"
	"reflect"
	"testing"
	"time"
	"unsafe"

	"github.com/mkc188/go-cache/v3/ttl"
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
	"weird": unsafe.Pointer(&ttl.Cache[string, string]{}),
	"float": 2.4,
	"url":   url.URL{},
}

func TestCache(t *testing.T) {
	// Prepare cache
	c := ttl.Cache[string, any]{}
	c.Init(
		len(testEntries),
		len(testEntries)+1,
		time.Second*5,
	)

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
		t.Logf("Cache.Add(%s, %v)", key, val)
		if !c.Add(key, val) {
			t.Fatalf("failed adding key to cache: %s", key)
		}
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
		if !c.CAS(key, val, nil, reflect.DeepEqual) && val != nil {
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
	time.Sleep(time.Second * 11)

	// Checking cache is off expected size
	t.Logf("Checking cache is of expected size (0)")
	if sz := c.Len(); sz != 0 {
		t.Fatalf("unexpected cache size: %d", sz)
	}

	// Update cache TTL time to be
	// sometime larger than sweep time.
	c.SetTTL(time.Second*20, false)

	// Restart the cache.
	for c.Stop() {
	}
	for c.Start(time.Second * 10) {
	}

	// Add all entries to cache
	for key, val := range testEntries {
		t.Logf("Cache.Add(%s, %v)", key, val)
		if !c.Add(key, val) {
			t.Fatalf("failed adding key to cache: %s", key)
		}
	}

	t.Log("Sleeping to give time for cache sweeps")
	time.Sleep(time.Second * 15)

	// Ensure all entries remain as expected
	for key := range testEntries {
		t.Logf("Cache.Has() => %s", key)
		if !c.Has(key) {
			t.Fatalf("key unexpectedly not found in cache: %s", key)
		}
	}

	t.Log("Sleeping to give time for cache sweeps")
	time.Sleep(time.Second * 11)

	// Checking cache is off expected size
	t.Logf("Checking cache is of expected size (0)")
	if sz := c.Len(); sz != 0 {
		t.Fatalf("unexpected cache size: %d", sz)
	}
}
