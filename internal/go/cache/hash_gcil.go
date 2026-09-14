package cache

import "sync"

var hashFileBufferPool = sync.Pool{
	New: func() any {
		return new([32 * 1024]byte)
	},
}

func SetSalt(b []byte) {
	hashSalt = b
}
