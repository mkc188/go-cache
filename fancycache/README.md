# go-fancycache

FancyCache is an example of a more complex cache implementation using `cache.TTLCache{}` as its underpinning.

It provides caching specifically of struct types, with automatic keying by multiple different field members, useful when wrapping a database.