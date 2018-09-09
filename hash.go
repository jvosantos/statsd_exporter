package main

const (
	offset64 = 14695981039346656037
	prime64  = 1099511628211
	separatorByte byte = 255
)

var (
	// cache the signature of an empty label set.
	emptyLabelSignature = hashNew()
)

// hashNew initializes a new fnv64a hash Value.
func hashNew() uint64 {
	return offset64
}

// hashAdd adds a string to a fnv64a hash Value, returning the updated hash.
func hashAdd(h uint64, s string) uint64 {
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime64
	}
	return h
}

// hashAddByte adds a byte to a fnv64a hash Value, returning the updated hash.
func hashAddByte(h uint64, b byte) uint64 {
	h ^= uint64(b)
	h *= prime64
	return h
}
