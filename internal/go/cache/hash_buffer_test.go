package cache

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileHashBufferBoundaries(t *testing.T) {
	for _, size := range []int{0, 1, 32767, 32768, 32769, 98311} {
		t.Run(fmt.Sprintf("bytes=%d", size), func(t *testing.T) {
			data := make([]byte, size)
			for i := range data {
				data[i] = byte(i)
			}
			name := filepath.Join(t.TempDir(), "source.go")
			require.NoError(t, os.WriteFile(name, data, 0o600))

			got, err := FileHash(name)
			require.NoError(t, err)
			assert.Equal(t, sha256.Sum256(data), got)
		})
	}
}

func TestFileHashReadErrorRetry(t *testing.T) {
	name := filepath.Join(t.TempDir(), "source.go")
	require.NoError(t, os.Mkdir(name, 0o700))
	got, err := FileHash(name)
	require.Error(t, err)
	assert.Zero(t, got)
	var pathErr *os.PathError
	require.ErrorAs(t, err, &pathErr)
	assert.Equal(t, "read", pathErr.Op)
	assert.Equal(t, name, pathErr.Path)

	require.NoError(t, os.Remove(name))
	data := []byte("package example\n")
	require.NoError(t, os.WriteFile(name, data, 0o600))
	got, err = FileHash(name)
	require.NoError(t, err)
	assert.Equal(t, sha256.Sum256(data), got)
}

func TestFileHashConcurrent(t *testing.T) {
	for i := range 16 {
		t.Run(fmt.Sprintf("file=%d", i), func(t *testing.T) {
			t.Parallel()

			data := bytes.Repeat([]byte{byte(i)}, 65537+i)
			name := filepath.Join(t.TempDir(), "source.go")
			require.NoError(t, os.WriteFile(name, data, 0o600))
			got, err := FileHash(name)
			require.NoError(t, err)
			assert.Equal(t, sha256.Sum256(data), got)
		})
	}
}
