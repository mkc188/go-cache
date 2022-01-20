package cache

// Hook defines a function hook that can be supplied as a callback.
type Hook[K, V comparable] func(key K, value V)

func emptyHook[K, V comparable](K, V) {}
