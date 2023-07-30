package result_test

import (
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"codeberg.org/gruf/go-cache/v3/result"
	"github.com/google/go-cmp/cmp"
)

const (
	testLookupField1      = "Field1"
	testLookupField2      = "Field2"
	testLookupField3      = "Field3"
	testLookupField4      = "Field4"
	testLookupField5And6  = "Field5.Field6"
	testLookupField7      = "Field7"
	testLookupField8      = "Field8"
	testLookupField9And10 = "Field9.Field10"
	testLookupField11     = "Field11"
	testLookupField12     = "Field12"
	testLookupField13     = "Field13"
	testLookupField14     = "Field14"
	testLookupField15     = "Field15"
	testLookupField16     = "Field16"
)

var testLookups = []struct {
	Lookup result.Lookup
	Fields func(testType) []any
}{
	{
		Lookup: result.Lookup{Name: testLookupField1, AllowZero: true},
		Fields: func(tt testType) []any { return []any{tt.Field1} },
	},
	{
		Lookup: result.Lookup{Name: testLookupField2, AllowZero: true},
		Fields: func(tt testType) []any { return []any{tt.Field2} },
	},
	{
		Lookup: result.Lookup{Name: testLookupField3, AllowZero: true},
		Fields: func(tt testType) []any { return []any{tt.Field3} },
	},
	{
		Lookup: result.Lookup{Name: testLookupField4, AllowZero: true},
		Fields: func(tt testType) []any { return []any{tt.Field4} },
	},
	{
		Lookup: result.Lookup{Name: testLookupField5And6, AllowZero: true},
		Fields: func(tt testType) []any { return []any{tt.Field5, tt.Field6} },
	},
	{
		Lookup: result.Lookup{Name: testLookupField7, AllowZero: true},
		Fields: func(tt testType) []any { return []any{tt.Field7} },
	},
	{
		Lookup: result.Lookup{Name: testLookupField8, AllowZero: true},
		Fields: func(tt testType) []any { return []any{tt.Field8} },
	},
	{
		Lookup: result.Lookup{Name: testLookupField9And10, AllowZero: true},
		Fields: func(tt testType) []any { return []any{tt.Field9, tt.Field10} },
	},
	{
		Lookup: result.Lookup{Name: testLookupField11, AllowZero: true},
		Fields: func(tt testType) []any { return []any{tt.Field11} },
	},
	{
		Lookup: result.Lookup{Name: testLookupField12, AllowZero: true},
		Fields: func(tt testType) []any { return []any{tt.Field12} },
	},
	{
		Lookup: result.Lookup{Name: testLookupField13, AllowZero: false},
		Fields: func(tt testType) []any { return []any{tt.Field13} },
	},
	{
		Lookup: result.Lookup{Name: testLookupField14, AllowZero: false},
		Fields: func(tt testType) []any { return []any{tt.Field14} },
	},
	{
		Lookup: result.Lookup{Name: testLookupField15, AllowZero: false},
		Fields: func(tt testType) []any { return []any{tt.Field15} },
	},
	{
		Lookup: result.Lookup{Name: testLookupField16, AllowZero: false},
		Fields: func(tt testType) []any { return []any{tt.Field16} },
	},
}

type testType struct {
	// Each must be unique
	Field1  string
	Field2  int
	Field3  uint
	Field4  float32
	Field7  time.Time
	Field8  *time.Time
	Field11 []byte
	Field12 []rune

	// Combined must be unique
	Field5  string
	Field6  string
	Field9  time.Duration
	Field10 *time.Duration

	// Empty, should be ignored
	Field13 int
	Field14 float32
	Field15 string
	Field16 []byte
}

var testEntries = []testType{
	{
		Field1:  "i am medium",
		Field2:  42,
		Field3:  69,
		Field4:  42.69,
		Field5:  "hello",
		Field6:  "world",
		Field7:  time.Time{}.Add(time.Nanosecond),
		Field8:  func() *time.Time { t := time.Time{}.Add(time.Nanosecond); return &t }(),
		Field9:  time.Nanosecond,
		Field10: func() *time.Duration { d := time.Nanosecond; return &d }(),
		Field11: []byte{'0'},
		Field12: []rune{'0'},
	},
	{
		Field1:  "i am small",
		Field2:  math.MinInt,
		Field3:  1,
		Field4:  math.SmallestNonzeroFloat32,
		Field5:  "hello",
		Field6:  "earth",
		Field7:  time.Time{}.Add(time.Millisecond),
		Field8:  func() *time.Time { t := time.Time{}.Add(time.Millisecond); return &t }(),
		Field9:  time.Millisecond,
		Field10: func() *time.Duration { d := time.Millisecond; return &d }(),
		Field11: []byte("hello world"),
		Field12: []rune("hello world"),
	},
	{
		Field1:  "i am large",
		Field2:  math.MaxInt,
		Field3:  math.MaxUint,
		Field4:  math.MaxFloat32,
		Field5:  "hello",
		Field6:  "moon",
		Field7:  time.Time{}.Add(time.Second),
		Field8:  func() *time.Time { t := time.Time{}.Add(time.Second); return &t }(),
		Field9:  time.Second,
		Field10: func() *time.Duration { d := time.Second; return &d }(),
		Field11: []byte{'\n'},
		Field12: []rune{'\n'},
	},
}

func TestCache(t *testing.T) {
	// Convert test lookups to lookup string slice
	lookups := func() []result.Lookup {
		var lookups []result.Lookup
		for _, l := range testLookups {
			lookups = append(lookups, l.Lookup)
		}
		return lookups
	}()

	// Prepare cache and schedule cleaning
	c := result.New(lookups, func(tt *testType) *testType {
		tt2 := new(testType)
		*tt2 = *tt
		return tt2
	}, 3)

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
					c.Has(lookup.Lookup.Name, lookup.Fields(entry)...)
				}
			}
		}
	}()
	defer close(done)

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
		t.Logf("Cache.Store(%+v)", testEntries[i])

		if err := c.Store(&(testEntries[i]), func() error {
			return nil
		}); err != nil {
			t.Fatalf("placing entry failed: %v", err)
		}
	}

	// Ensure all entries are expected
	for _, entry := range testEntries {
		for _, lookup := range testLookups {
			key := lookup.Fields(entry)
			zero := true

			// Skip zero value keys
			for _, field := range key {
				zero = zero && reflect.ValueOf(field).IsZero()
			}
			if zero {
				continue
			}

			check, err := c.Load(lookup.Lookup.Name, func() (*testType, error) {
				return nil, errors.New("item SHOULD be cached")
			}, lookup.Fields(entry)...)
			if err != nil {
				t.Errorf("key unexpectedly not found in cache: %v", err)
			} else if !cmp.Equal(entry, *check) {
				t.Errorf("value not as expected for key in cache: %s", lookup.Lookup.Name)
			}
		}
	}

	// Force invalidate, check callbacks
	for _, entry := range testEntries {
		lookup := testLookups[0].Lookup
		key := testLookups[0].Fields(entry)

		t.Logf("Cache.Invalidate(%s,%v)", lookup.Name, key)
		c.Invalidate(lookup.Name, key...)

		if !findInCallbacks(callbacks, entry) {
			t.Errorf("invalidate callback unexpectedly not called for: %s,%v", lookup.Name, key)
		}

		for _, lookup := range testLookups {
			key := lookup.Fields(entry)
			if c.Has(lookup.Lookup.Name, key...) {
				t.Errorf("key unexpected found in cache: %s,%v", lookup.Lookup.Name, key)
			}
		}
	}

	// Reset callbacks
	callbacks = callbacks[:0]

	// Re-add all entries to cache
	for i := range testEntries {
		t.Logf("Cache.Store(%+v)", testEntries[i])

		if err := c.Store(&(testEntries[i]), func() error {
			return nil
		}); err != nil {
			t.Fatalf("placing entry failed: %v", err)
		}
	}
}
