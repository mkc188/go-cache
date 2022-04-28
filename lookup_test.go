package cache_test

import (
	"testing"
	"time"
	"unsafe"

	"codeberg.org/gruf/go-cache/v2"
	"github.com/google/go-cmp/cmp"
)

type testEntry struct {
	Key1  string
	Key2  string
	Key3  string
	Key4  string
	Value interface{}
}

var testLookupEntries = []*testEntry{
	{
		Key1:  "key11",
		Key2:  "key12",
		Key3:  "key13",
		Key4:  "key14",
		Value: 1,
	},
	{
		Key1:  "key21",
		Key2:  "key22",
		Key3:  "key23",
		Key4:  "key24",
		Value: "value",
	},
	{
		Key1:  "key31",
		Key2:  "key32",
		Key3:  "key33",
		Key4:  "key34",
		Value: []string{"1", "2"},
	},
	{
		Key1:  "key41",
		Key2:  "key42",
		Key3:  "key43",
		Key4:  "key44",
		Value: []interface{}{0, 1.1, -99, "aaaa"},
	},
	{
		Key1:  "key51",
		Key2:  "key52",
		Key3:  "key53",
		Key4:  "key54",
		Value: map[string]string{"hello": "world"},
	},
	{
		Key1:  "key61",
		Key2:  "key62",
		Key3:  "key63",
		Key4:  "key64",
		Value: struct{ Field string }{"field"},
	},
	{
		Key1:  "key71",
		Key2:  "key72",
		Key3:  "key73",
		Key4:  "key74",
		Value: unsafe.Pointer(nil),
	},
	{
		Key1:  "key81",
		Key2:  "key82",
		Key3:  "key83",
		Key4:  "key84",
		Value: []byte{'0', '1', '2'},
	},
	{
		Key1:  "key91",
		Key2:  "key92",
		Key3:  "key93",
		Key4:  "key94",
		Value: '0',
	},
	{
		Key1:  "key101",
		Key2:  "key102",
		Key3:  "key103",
		Key4:  "key104",
		Value: nil,
	},
}

func TestLookupCache(t *testing.T) {
	// Prepare cache
	c := cache.NewLookup(cache.LookupCfg[string, string, interface{}]{
		RegisterLookups: func(lm *cache.LookupMap[string, string]) {
			lm.RegisterLookup("key2")
			lm.RegisterLookup("key3")
			lm.RegisterLookup("key4")
		},
		AddLookups: func(lm *cache.LookupMap[string, string], i interface{}) {
			e := i.(*testEntry)
			lm.Set("key2", e.Key2, e.Key1)
			lm.Set("key3", e.Key3, e.Key1)
			lm.Set("key4", e.Key4, e.Key1)
		},
		DeleteLookups: func(lm *cache.LookupMap[string, string], i interface{}) {
			e := i.(*testEntry)
			if e.Key2 != "" {
				lm.Delete("key2", e.Key2)
			}
			if e.Key3 != "" {
				lm.Delete("key3", e.Key3)
			}
			if e.Key4 != "" {
				lm.Delete("key4", e.Key4)
			}
		},
	})
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
			for _, entry := range testLookupEntries {
				c.Has(entry.Key1)
				c.HasBy("key2", entry.Key2)
				c.HasBy("key3", entry.Key3)
				c.HasBy("key4", entry.Key4)
			}
		}
	}()

	// Track callbacks set
	callbacks := map[string]interface{}{}
	c.SetInvalidateCallback(func(key string, value interface{}) {
		callbacks[key] = value
	})

	// Add all entries to cache
	for _, val := range testLookupEntries {
		t.Logf("Cache.Put(%v)", val)
		c.Put(val.Key1, val)
	}

	// Ensure all entries are expected
	for _, val := range testLookupEntries {
		check, ok := c.Get(val.Key1)
		t.Logf("Cache.Get() => %v", val)
		if !ok {
			t.Fatalf("key unexpectedly not found in cache: %s", val.Key1)
		} else if !cmp.Equal(val, check) {
			t.Fatalf("value not as expected for key in cache: %s", val.Key1)
		}
	}

	// Force invalidate, check callbacks
	for _, entry := range testLookupEntries {
		key := entry.Key1

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
