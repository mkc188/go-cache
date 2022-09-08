package fancycache

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"codeberg.org/gruf/go-byteutil"
	"github.com/kelindar/binary"
)

// structKeys provides convience methods for a list
// of struct field combinations used for cache keys.
type structKeys []keyFields

// get fetches the key-fields for given prefix (else, panics).
func (sk structKeys) get(prefix string) *keyFields {
	for i := range sk {
		if sk[i].prefix == prefix {
			return &sk[i]
		}
	}
	panic("unknown lookup (key prefix): \"" + prefix + "\"")
}

// generate will calculate the value string for each required
// cache key as laid-out by the receiving structKeys{}.
func (sk structKeys) generate(v any) []cacheKey {
	// Get reflected value in order
	// to access the struct fields
	rv := reflect.ValueOf(v)

	// Iteratively deref pointer value
	for rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
		if rv.IsZero() {
			panic("nil ptr")
		}
	}

	// Preallocate expected slice of keys
	keys := make([]cacheKey, len(sk))

	// Acquire binary encoder
	enc := encpool.Get().(*encoder)
	defer encpool.Put(enc)

	for i := range sk {
		// Reset encoder
		enc.Reset()

		// Set the key-fields reference
		keys[i].fields = &sk[i]

		// Calculate cache-key value
		keys[i].populate(enc, rv)
	}

	return keys
}

// cacheKey represents an actual cache key.
type cacheKey struct {
	// value is the actual string representing
	// this cache key for hashmap lookups.
	value string

	// fieldsRO is a read-only slice (i.e. we should
	// NOT be modifying them, only using for reference)
	// of struct fields encapsulated by this cache key.
	fields *keyFields
}

// populate will calculate the cache key's value string for given
// value's reflected information. Passed encoder is for string building.
func (k *cacheKey) populate(enc *encoder, v reflect.Value) {
	// Append precalculated prefix
	enc.AppendString(k.fields.prefix)
	enc.AppendByte('.')

	// Append each field value to buffer.
	for _, idx := range k.fields.fields {
		fv := v.Field(idx)
		fi := fv.Interface()
		enc.Encode(fi)
	}

	// Create copy of enc's value
	k.value = enc.String()
}

// keyFields represents a list of struct fields
// encompassed in a single cache key, including
// the string used as they key's prefix.
type keyFields struct {
	// prefix is the calculated (well, provided)
	// cache key prefix, consisting of dot sep'd
	// struct field names.
	prefix string

	// fields is a slice of runtime struct field
	// indices, of the fields encompassed by this key.
	fields []int
}

// populate will populate this keyFields{} object's .fields member by determining
// the field names from current prefix, and querying given reflected type to get
// the runtime field indices for each of the fields. this speeds-up future value lookups.
func (kf *keyFields) populate(t reflect.Type) {
	// Split dot-separated prefix to get
	// the individual struct field names
	names := strings.Split(kf.prefix, ".")
	if len(names) < 1 {
		panic("no key fields specified")
	}

	// Pre-allocate slice of expected length
	kf.fields = make([]int, len(names))

	for i, name := range names {
		// Get field info for given name
		ft, ok := t.FieldByName(name)
		if !ok {
			panic("no field found for name: \"" + name + "\"")
		}

		// Check field is usable
		if !isExported(name) {
			panic("field must be exported")
		}

		// Set the runtime field index
		kf.fields[i] = ft.Index[0]
	}
}

// genkey generates a cache key for given lookup and key value.
func genkey(lookup string, parts ...any) string {
	if len(parts) < 1 {
		// Panic to prevent annoying usecase
		// where user forgets to pass lookup
		// and instead only passes a key part,
		// e.g. cache.Get("key")
		// which then always returns false.
		panic("no key parts provided")
	}

	// Acquire encoder and reset
	enc := encpool.Get().(*encoder)
	defer encpool.Put(enc)
	enc.Reset()

	// Append the lookup prefix
	enc.AppendString(lookup)
	enc.AppendByte('.')

	// Encode each key part
	for _, part := range parts {
		enc.Encode(part)
	}

	return enc.String()
}

// encpool is a memory pool of binary encoders
// with cached codecs to speed-up encoding.
var encpool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 512)
		return &encoder{
			enc: binary.NewEncoder(nil),
			buf: byteutil.Buffer{B: b},
		}
	},
}

// encoder wraps a binary encoder and byte buffer
// to provide easy access to both from a singular
// / memory pool. The encoder caches binary codecs
// and the byte buffer provides an output location
// when encoding values to binary for cache keys.
type encoder struct {
	enc *binary.Encoder
	buf byteutil.Buffer
}

func (enc *encoder) Encode(v any) {
	// Reset to clear errors
	enc.enc.Reset(&enc.buf)

	// Encode value, accept no errors
	if err := enc.enc.Encode(v); err != nil {
		panic(fmt.Errorf("invalid key: %w", err))
	}
}

func (enc *encoder) AppendByte(b byte) {
	_ = enc.buf.WriteByte(b)
}

func (enc *encoder) AppendBytes(b []byte) {
	enc.buf.B = append(enc.buf.B, b...)
	_, _ = enc.buf.Write(b)
}

func (enc *encoder) AppendString(s string) {
	_, _ = enc.buf.WriteString(s)
}

func (enc *encoder) String() string {
	return string(enc.buf.B)
}

func (enc *encoder) Reset() {
	enc.buf.Reset()
}

// isExported checks whether function name is exported.
func isExported(fnName string) bool {
	r, _ := utf8.DecodeRuneInString(fnName)
	return unicode.IsUpper(r)
}
