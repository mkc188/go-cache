package fancycache

import (
	"encoding/binary"
	"fmt"
	"math/bits"
	"net"
	"net/netip"
	"reflect"
	"time"
	"unsafe"

	"github.com/alphadose/haxmap"
)

var (
	// encoders is a map of runtime type ptrs => encoder functions.
	encoders = haxmap.New[uintptr, encoder_iface](50)

	// bin is a short-hand for our chosen byteorder encoding.
	bin = binary.LittleEndian
)

// encoder_iface functions will encode value contained in interface{} box into buffer.
type encoder_iface func([]byte, any) []byte

// encoder_reflect functions will encode value contained in reflected form into buffer.
type encoder_reflect func([]byte, reflect.Value) []byte

// encode will append a simple encoded form of 'a' to 'buf', using a cache to
// calculate and store encoder functions per type. Panics on unsupported types.
//
// NOTES:
//   - we do not need to worry about unexported struct fields during reflection,
//     as we do not iterate struct types and only allow exported fields passed to us
//   - there would not be any performance improvement replacing encoder_iface to use
//     unsafe.Pointer instead of interface{}, as we will always have a boxed interface{}
//     type coming in, which will then be passed until it is either reflect.ValueOf()'d
//     or the value is pulled out using iface_value().
//   - the core focus of our encoding here is speed while supporting as many types
//     as we reasonably can. it is not worthing hashing each value here as the end
//     result is being stored in a hashmap under a string key, which itself will perform
//     the hashing.
func encode(buf []byte, a any) []byte {
	// Get reflect type of 'a'
	t := reflect.TypeOf(a)

	// Get raw runtime type ptr
	ptr := iface_value(t)
	uptr := uintptr(ptr)

	// Look for a cached encoder
	enc, ok := encoders.Get(uptr)
	if ok {
		return enc(buf, a)
	}

	// Search by type switch
	enc, ok = loadSimple(a)

	if !ok {
		// Search by reflected type
		renc, ok := loadReflect(t)
		if !ok {
			panic("invalid type: " + t.String())
		}

		// Wrap encoder to reflect value
		enc = func(buf []byte, a any) []byte {
			return renc(buf, reflect.ValueOf(a))
		}
	}

	// Store encoder in cache
	encoders.Set(uptr, enc)
	return enc(buf, a)
}

// loadSimple loads an encoder func for type of given value, using a simple type switch.
func loadSimple(a any) (encoder_iface, bool) {
	switch a.(type) {
	// String-like types
	case string, []byte:
		return encode_string, true
	case *string, *[]byte:
		return func(buf []byte, a any) []byte {
			ptr := (*string)(iface_value(a))
			if ptr == nil {
				return buf
			}
			return encode_string(buf, *ptr)
		}, true

	// Boolean types
	case bool:
		return encode_bool, true
	case *bool:
		return func(buf []byte, a any) []byte {
			ptr := (*bool)(iface_value(a))
			if ptr == nil {
				return buf
			}
			return encode_bool(buf, *ptr)
		}, true

	// Platform int size types
	case int, uint, uintptr:
		return encode_platform_int, true
	case *int, *uint, *uintptr:
		return func(buf []byte, a any) []byte {
			ptr := (*int)(iface_value(a))
			if ptr == nil {
				return buf
			}
			return encode_platform_int(buf, *ptr)
		}, true

	// 8bit integer types
	case int8, uint8:
		return encode_int8, true
	case *int8, *uint8:
		return func(buf []byte, a any) []byte {
			ptr := (*int8)(iface_value(a))
			if ptr == nil {
				return buf
			}
			return encode_int8(buf, *ptr)
		}, true

	// 16bit integer types
	case int16, uint16:
		return encode_int16, true
	case *int16, *uint16:
		return func(buf []byte, a any) []byte {
			ptr := (*int16)(iface_value(a))
			if ptr == nil {
				return buf
			}
			return encode_int16(buf, *ptr)
		}, true

	// 32bit integer types
	case int32, uint32:
		return encode_int32, true
	case *int32, *uint32:
		return func(buf []byte, a any) []byte {
			ptr := (*int32)(iface_value(a))
			if ptr == nil {
				return buf
			}
			return encode_int32(buf, *ptr)
		}, true

	// 64bit integer types
	case int64, uint64, time.Duration:
		return encode_int64, true
	case *int64, *uint64, *time.Duration:
		return func(buf []byte, a any) []byte {
			ptr := (*int64)(iface_value(a))
			if ptr == nil {
				return buf
			}
			return encode_int64(buf, *ptr)
		}, true

	// 32bit float types
	case float32:
		return encode_float32, true
	case *float32:
		return func(buf []byte, a any) []byte {
			ptr := (*float32)(iface_value(a))
			if ptr == nil {
				return buf
			}
			return encode_float32(buf, *ptr)
		}, true

	// 64bit float types
	case float64:
		return encode_float64, true
	case *float64:
		return func(buf []byte, a any) []byte {
			ptr := (*float64)(iface_value(a))
			if ptr == nil {
				return buf
			}
			return encode_float64(buf, *ptr)
		}, true

	// 64bit complex types
	case complex64:
		return encode_complex64, true
	case *complex64:
		return func(buf []byte, a any) []byte {
			ptr := (*complex64)(iface_value(a))
			if ptr == nil {
				return buf
			}
			return encode_complex64(buf, *ptr)
		}, true

	// 128bit complex types
	case complex128:
		return encode_complex128, true
	case *complex128:
		return func(buf []byte, a any) []byte {
			ptr := (*complex128)(iface_value(a))
			if ptr == nil {
				return buf
			}
			return encode_complex128(buf, *ptr)
		}, true

	// time.Time types
	case time.Time:
		return encode_time, true
	case *time.Time:
		return func(buf []byte, a any) []byte {
			ptr := (*time.Time)(iface_value(a))
			if ptr == nil {
				return buf
			}
			return encode_time(buf, *ptr)
		}, true

	// net.IP types
	case net.IP:
		return encode_ip, true
	case *net.IP:
		return func(buf []byte, a any) []byte {
			ptr := (*net.IP)(iface_value(a))
			if ptr == nil {
				return buf
			}
			return encode_ip(buf, *ptr)
		}, true

	// netip.Addr types
	case netip.Addr:
		return encode_addr, true
	case *netip.Addr:
		return func(buf []byte, a any) []byte {
			ptr := (*netip.Addr)(iface_value(a))
			if ptr == nil {
				return buf
			}
			return encode_addr(buf, *ptr)
		}, true

	// Interface types
	case fmt.Stringer:
		return encode_stringer, true

	default:
		return nil, false
	}
}

// loadReflect loads an encoder func for a value of given reflected type.
func loadReflect(t reflect.Type) (encoder_reflect, bool) {
	switch t.Kind() {
	case reflect.Pointer:
		// Element
		et := t

		// Fully dereference pointer type
		for et.Kind() == reflect.Pointer {
			et = et.Elem()
		}

		// Get elem iface + type
		ev := reflect.New(et).Elem()
		ea := ev.Interface()

		// Try simple encoder load
		enc, ok := loadSimple(ea)
		if ok {
			return deref_ptr_iface(enc), true
		}

		// Try reflecct encoder load
		renc, ok := loadReflect(et)
		if ok {
			return deref_ptr_reflect(renc), true
		}

		return nil, false

	case reflect.String:
		return func(buf []byte, v reflect.Value) []byte {
			return encode_string(buf, v.String())
		}, true

	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			// Special case of byte slice (we cast to string)
			return func(buf []byte, v reflect.Value) []byte {
				return encode_string(buf, v.Bytes())
			}, true
		}

		// Handle as all other arrays
		fallthrough

	case reflect.Array:
		// Element
		et := t.Elem()

		// Get elem iface + type
		ev := reflect.New(et).Elem()
		ea := ev.Interface()

		// Try simple encoder load
		enc, ok := loadSimple(ea)
		if ok {
			return iter_slice_iface(enc), true
		}

		// Try reflect encoder load
		renc, ok := loadReflect(et)
		if ok {
			return iter_slice_reflect(renc), true
		}

		return nil, false

	case reflect.Bool:
		return func(buf []byte, v reflect.Value) []byte {
			return encode_bool(buf, v.Bool())
		}, true

	case reflect.Int,
		reflect.Int8,
		reflect.Int16,
		reflect.Int32,
		reflect.Int64:
		return func(buf []byte, v reflect.Value) []byte {
			return encode_int64(buf, v.Int())
		}, true

	case reflect.Uint,
		reflect.Uint8,
		reflect.Uint16,
		reflect.Uint32,
		reflect.Uint64,
		reflect.Uintptr:
		return func(buf []byte, v reflect.Value) []byte {
			return encode_int64(buf, v.Uint())
		}, true

	case reflect.Float32,
		reflect.Float64:
		return func(buf []byte, v reflect.Value) []byte {
			return encode_float64(buf, v.Float())
		}, true

	case reflect.Complex64,
		reflect.Complex128:
		return func(buf []byte, v reflect.Value) []byte {
			return encode_complex128(buf, v.Complex())
		}, true

	default:
		return nil, false
	}
}

func encode_string(buf []byte, a any) []byte {
	return append(buf, *(*string)(iface_value(a))...)
}

func encode_bool(buf []byte, a any) []byte {
	if *(*bool)(iface_value(a)) {
		return append(buf, '1')
	}
	return append(buf, '0')
}

func encode_int8(buf []byte, a any) []byte {
	return append(buf, *(*uint8)(iface_value(a)))
}

func encode_int16(buf []byte, a any) []byte {
	return bin.AppendUint16(buf, *(*uint16)(iface_value(a)))
}

func encode_int32(buf []byte, a any) []byte {
	return bin.AppendUint32(buf, *(*uint32)(iface_value(a)))
}

func encode_int64(buf []byte, a any) []byte {
	return bin.AppendUint64(buf, *(*uint64)(iface_value(a)))
}

// encode_platform_int containers the correct iface encoder on runtime for platform int size.
var encode_platform_int = func() encoder_iface {
	switch bits.UintSize {
	case 32:
		return encode_int32
	case 64:
		return encode_int64
	default:
		panic("unexpected platform int size")
	}
}()

func encode_float32(buf []byte, a any) []byte {
	// we force runtime to see float64's memory as int32...
	// NOTE: this only works as below func force casts via unsafe.
	return encode_int32(buf, a)
}

func encode_float64(buf []byte, a any) []byte {
	// we force runtime to see float64's memory as int64...
	// NOTE: this only works as below func force casts via unsafe.
	return encode_int64(buf, a)
}

func encode_complex64(buf []byte, a any) []byte {
	c := *(*complex64)(iface_value(a))
	buf = encode_float32(buf, real(c))
	buf = encode_float32(buf, imag(c))
	return buf
}

func encode_complex128(buf []byte, a any) []byte {
	c := *(*complex128)(iface_value(a))
	buf = encode_float64(buf, real(c))
	buf = encode_float64(buf, imag(c))
	return buf
}

func encode_time(buf []byte, a any) []byte {
	t := *(*time.Time)(iface_value(a))
	return encode_int64(buf, t.UnixNano())
}

func encode_ip(buf []byte, a any) []byte {
	ip := *(*net.IP)(iface_value(a))
	return append(buf, ip[:]...)
}

func encode_addr(buf []byte, a any) []byte {
	addr := *(*netip.Addr)(iface_value(a))
	return append(buf, addr.AsSlice()...)
}

func encode_stringer(buf []byte, a any) []byte {
	v := *(*fmt.Stringer)(iface_value(a))
	if v == nil {
		return buf
	}
	return encode_string(buf, v.String())
}

// deref_ptr_iface wraps an iface encoder to fully dereference the passed reflected value.
func deref_ptr_iface(enc encoder_iface) encoder_reflect {
	return func(buf []byte, v reflect.Value) []byte {
		for v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return buf
			}
			v = v.Elem()
		}
		return enc(buf, v.Interface())
	}
}

// deref_ptr_reflect wraps a reflect encoder to fully dereference the passed reflected value.
func deref_ptr_reflect(renc encoder_reflect) encoder_reflect {
	return func(buf []byte, v reflect.Value) []byte {
		for v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return buf
			}
			v = v.Elem()
		}
		return renc(buf, v)
	}
}

// iter_slice_iface wraps an iface encoder to iterate the reflected value's
// slice/array elements and pass the resulting element to given encoder.
func iter_slice_iface(enc encoder_iface) encoder_reflect {
	return func(buf []byte, v reflect.Value) []byte {
		for i := 0; i < v.Len(); i++ {
			buf = enc(buf, v.Index(i).Interface())
		}
		return buf
	}
}

// iter_slice_reflect wraps a reflect encoder to iterate the reflected value's
// slice/array elements and pass the resulting element to given encoder.
func iter_slice_reflect(renc encoder_reflect) encoder_reflect {
	return func(buf []byte, v reflect.Value) []byte {
		for i := 0; i < v.Len(); i++ {
			buf = renc(buf, v.Index(i))
		}
		return buf
	}
}

// iface_value returns the raw pointer for a value boxed within interface{} type.
func iface_value(a any) unsafe.Pointer {
	type eface struct {
		Type  unsafe.Pointer
		Value unsafe.Pointer
	}
	return (*eface)(unsafe.Pointer(&a)).Value
}
