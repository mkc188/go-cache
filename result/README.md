# result

`result.Cache` is an example of a more complex cache implementation using `cache.TTLCache{}` as its underpinning.

It provides caching specifically of loadable struct types, with automatic keying by multiple different field members and caching of negative (error) values. All useful when wrapping, for example, a database.