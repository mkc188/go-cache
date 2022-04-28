package cache

import "unsafe"

// Iface represents the underlying fat pointer structure
// the runtime holds for an interface. This allows using
// an interface as a value type in a cache (which normally)
// doesn't implement the comparable interface.
type Iface struct {
	Type  unsafe.Pointer
	Value unsafe.Pointer
}

// ToIface converts an interface to Iface representation.
func ToIface(i interface{}) *Iface {
	return (*Iface)(unsafe.Pointer(&i))
}

// Nil will return if this Iface represents a nil value.
func (i *Iface) Nil() bool {
	return i == nil || i.Value == nil
}

// Interface returns the interface this Iface represents.
func (i *Iface) Interface() interface{} {
	return *(*interface{})(unsafe.Pointer(i))
}
