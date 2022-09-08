package fancycache_test

import (
	"math"
	"testing"
	"time"

	"codeberg.org/gruf/go-cache/v3/fancycache"
	"github.com/google/go-cmp/cmp"
)

const (
	testLookupField1     = "Field1"
	testLookupField2     = "Field2"
	testLookupField3     = "Field3"
	testLookupField4     = "Field4"
	testLookupField5And6 = "Field5.Field6"
)

var testLookups = []struct {
	Lookup string
	Fields func(testType) []any
}{
	{
		Lookup: testLookupField1,
		Fields: func(tt testType) []any { return []any{tt.Field1} },
	},
	{
		Lookup: testLookupField2,
		Fields: func(tt testType) []any { return []any{tt.Field2} },
	},
	{
		Lookup: testLookupField3,
		Fields: func(tt testType) []any { return []any{tt.Field3} },
	},
	{
		Lookup: testLookupField4,
		Fields: func(tt testType) []any { return []any{tt.Field4} },
	},
	{
		Lookup: testLookupField5And6,
		Fields: func(tt testType) []any { return []any{tt.Field5, tt.Field6} },
	},
}

type testType struct {
	// Each must be unique
	Field1 string
	Field2 int
	Field3 uint
	Field4 float32

	// Combined must be unique
	Field5 string
	Field6 string
}

var testEntries = []testType{
	{
		Field1: "i am medium",
		Field2: 42,
		Field3: 69,
		Field4: 42.69,
		Field5: "hello",
		Field6: "world",
	},
	{
		Field1: "i am small",
		Field2: math.MinInt,
		Field3: 0,
		Field4: math.SmallestNonzeroFloat32,
		Field5: "hello",
		Field6: "earth",
	},
	{
		Field1: "i am large",
		Field2: math.MaxInt,
		Field3: math.MaxUint,
		Field4: math.MaxFloat32,
		Field5: "hello",
		Field6: "moon",
	},
}

func TestCache(t *testing.T) {
	// Convert test lookups to lookup string slice
	lookups := func() []string {
		var lookups []string
		for _, l := range testLookups {
			lookups = append(lookups, l.Lookup)
		}
		return lookups
	}()

	// Prepare cache and schedule cleaning
	c := fancycache.New[*testType](10, lookups)
	c.SetTTL(time.Second*5, false)
	_ = c.Start(time.Second * 10)

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
			// (puts concurrent strain on cache)
			for _, entry := range testEntries {
				for _, lookup := range testLookups {
					c.Has(lookup.Lookup, lookup.Fields(entry)...)
				}
			}
		}
	}()

	// Allocate callbacks slice of length >= expected.
	callbacks := make([]testType, 0, len(testEntries))

	// Track callbacks performed
	c.SetInvalidateCallback(func(tt *testType) {
		t.Logf("-> Invalidate: %+v", tt)
		callbacks = append(callbacks, *tt)
	})
	c.SetEvictionCallback(func(tt *testType) {
		t.Logf("-> Evict: %+v", tt)
		callbacks = append(callbacks, *tt)
	})

	// Prepare callback search function
	findInCallbacks := func(cb []testType, tt testType) bool {
		for _, entry := range cb {
			if cmp.Equal(entry, tt) {
				return true
			}
		}
		return false
	}

	// Add all entries to cache
	for i := range testEntries {
		t.Logf("Cache.Put(%+v)", testEntries[i])
		c.Put(&(testEntries[i]))
	}

	// Ensure all entries are expected
	for _, entry := range testEntries {
		for _, lookup := range testLookups {
			key := lookup.Fields(entry)
			check, ok := c.Get(lookup.Lookup, key...)
			t.Logf("Cache.Get(%s,%v)", lookup.Lookup, key)
			if !ok {
				t.Errorf("key unexpectedly not found in cache: %s,%v", lookup.Lookup, key)
			} else if !cmp.Equal(entry, *check) {
				t.Errorf("value not as expected for key in cache: %s,%v", lookup.Lookup, key)
			}
		}
	}

	// Force invalidate, check callbacks
	for _, entry := range testEntries {
		lookup := testLookups[0].Lookup
		key := testLookups[0].Fields(entry)

		t.Logf("Cache.Invalidate(%s,%v)", lookup, key)
		c.Invalidate(lookup, key...)

		if !findInCallbacks(callbacks, entry) {
			t.Errorf("invalidate callback unexpectedly not called for: %s,%v", lookup, key)
		}

		for _, lookup := range testLookups {
			key := lookup.Fields(entry)
			if c.Has(lookup.Lookup, key...) {
				t.Errorf("key unexpected found in cache: %s,%v", lookup.Lookup, key)
			}
		}
	}

	// Reset callbacks
	callbacks = callbacks[:0]

	// Re-add all entries to cache
	for i := range testEntries {
		t.Logf("Cache.Put(%+v)", testEntries[i])
		c.Put(&(testEntries[i]))
	}

	close(done) // stop the background loop
	t.Log("Sleeping to give time for cache sweeps")
	time.Sleep(time.Second * 15)

	// Check callbacks for evicted entries
	for _, entry := range testEntries {
		lookup := testLookups[0].Lookup
		key := testLookups[0].Fields(entry)
		if !findInCallbacks(callbacks, entry) {
			t.Errorf("evict callback unexpectedly not called for: %s,%v", lookup, key)
		}
	}
}

func BenchmarkCacheGet(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
		}
	})
}

func BenchmarkCacheHas(b *testing.B) {
}

func BenchmarkCachePut(b *testing.B) {
}

func BenchmarkCacheInvalidate(b *testing.B) {
}

func BenchmarkCacheConcurrentUse(b *testing.B) {
}
