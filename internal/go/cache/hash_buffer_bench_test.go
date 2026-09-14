package cache

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func BenchmarkFileHashUncached(b *testing.B) {
	for _, size := range []int{1024, 32768, 1048576} {
		b.Run(fmt.Sprintf("bytes=%d", size), func(b *testing.B) {
			name := filepath.Join(b.TempDir(), "source.go")
			require.NoError(b, os.WriteFile(name, bytes.Repeat([]byte("x"), size), 0o600))
			b.ReportAllocs()
			b.SetBytes(int64(size))

			for b.Loop() {
				hashFileCache.Lock()
				delete(hashFileCache.m, name)
				hashFileCache.Unlock()

				_, err := FileHash(name)
				require.NoError(b, err)
			}
		})
	}
}

func BenchmarkFileHashCached(b *testing.B) {
	name := filepath.Join(b.TempDir(), "source.go")
	require.NoError(b, os.WriteFile(name, []byte("package example\n"), 0o600))
	_, err := FileHash(name)
	require.NoError(b, err)
	b.ReportAllocs()

	for b.Loop() {
		_, err := FileHash(name)
		require.NoError(b, err)
	}
}
