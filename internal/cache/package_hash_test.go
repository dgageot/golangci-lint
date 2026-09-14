package cache

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

func TestPackageHashConcurrentDiamond(t *testing.T) {
	file := filepath.Join(t.TempDir(), "shared.go")
	require.NoError(t, os.WriteFile(file, []byte("package shared\n"), 0o600))
	shared := &packages.Package{PkgPath: "shared", CompiledGoFiles: []string{file}}
	roots := []*packages.Package{
		{PkgPath: "left", Imports: map[string]*packages.Package{"shared": shared}},
		{PkgPath: "right", Imports: map[string]*packages.Package{"shared": shared}},
	}
	modes := []HashMode{HashModeNeedOnlySelf, HashModeNeedDirectDeps, HashModeNeedAllDeps}
	sequential := &Cache{ioSem: make(chan struct{}, 1)}
	var expected [2][3]string
	for i, pkg := range roots {
		for j, mode := range modes {
			hash, err := sequential.packageHash(pkg, mode)
			require.NoError(t, err)
			expected[i][j] = hash
		}
	}

	cache := &Cache{ioSem: make(chan struct{}, 1)}
	var wg sync.WaitGroup
	for i := range 24 {
		wg.Go(func() {
			root, mode := i%len(roots), i%len(modes)
			hash, err := cache.packageHash(roots[root], modes[mode])
			if assert.NoError(t, err) {
				assert.Equal(t, expected[root][mode], hash)
			}
		})
	}
	wg.Wait()
}

func TestPackageHashRetryAfterDependencyError(t *testing.T) {
	file := filepath.Join(t.TempDir(), "missing.go")
	dep := &packages.Package{PkgPath: "dep", Name: "dep", CompiledGoFiles: []string{file}}
	pkg := &packages.Package{PkgPath: "example", Imports: map[string]*packages.Package{"dep": dep}}
	cache := &Cache{ioSem: make(chan struct{}, 1)}

	_, err := cache.packageHash(pkg, HashModeNeedAllDeps)
	require.ErrorIs(t, err, os.ErrNotExist)
	for _, p := range []*packages.Package{pkg, dep} {
		_, cached := cache.pkgHashes.Load(p)
		assert.False(t, cached)
	}

	require.NoError(t, os.WriteFile(file, []byte("package dep\n"), 0o600))
	hash, err := cache.packageHash(pkg, HashModeNeedAllDeps)
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
}

func TestPackageHashConcurrentFailureRetry(t *testing.T) {
	file := filepath.Join(t.TempDir(), "missing.go")
	pkg := &packages.Package{PkgPath: "example", CompiledGoFiles: []string{file}}
	cache := &Cache{ioSem: make(chan struct{}, 1)}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			_, err := cache.packageHash(pkg, HashModeNeedAllDeps)
			assert.ErrorIs(t, err, os.ErrNotExist)
		})
	}
	wg.Wait()

	require.NoError(t, os.WriteFile(file, []byte("package example\n"), 0o600))
	hash, err := cache.packageHash(pkg, HashModeNeedAllDeps)
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
}

func TestPackageHashUnlocksAfterPanic(t *testing.T) {
	dep := &packages.Package{PkgPath: "dep"}
	pkg := &packages.Package{PkgPath: "example", Imports: map[string]*packages.Package{"dep": dep}}
	cache := &Cache{}
	// Panic on the dependency result while the parent lock is held.
	cache.pkgHashes.Store(dep, "invalid result")

	require.Panics(t, func() {
		_, _ = cache.packageHash(pkg, HashModeNeedAllDeps)
	})
	cache.pkgHashes.Delete(dep)

	hash, err := cache.packageHash(pkg, HashModeNeedAllDeps)
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
}

func TestPackageHashSamePathVariants(t *testing.T) {
	cache := &Cache{ioSem: make(chan struct{}, 1)}
	base := &packages.Package{PkgPath: "example", ID: "example"}
	variant := &packages.Package{
		PkgPath: "example",
		ID:      "example [example.test]",
		Imports: map[string]*packages.Package{"testing": {PkgPath: "testing"}},
	}
	baseHash, err := cache.packageHash(base, HashModeNeedAllDeps)
	require.NoError(t, err)
	variantHash, err := cache.packageHash(variant, HashModeNeedAllDeps)
	require.NoError(t, err)
	assert.NotEqual(t, baseHash, variantHash)
}

func BenchmarkPackageHashSharedDependencies(b *testing.B) {
	for _, workers := range []int{1, 8, 16} {
		b.Run(fmt.Sprintf("workers=%d", workers), func(b *testing.B) {
			var dependency *packages.Package
			for i := range 64 {
				pkg := &packages.Package{PkgPath: fmt.Sprintf("dep%d", i)}
				if dependency != nil {
					pkg.Imports = map[string]*packages.Package{dependency.PkgPath: dependency}
				}
				dependency = pkg
			}
			var roots []*packages.Package
			for i := range workers {
				roots = append(roots, &packages.Package{
					PkgPath: fmt.Sprintf("root%d", i),
					Imports: map[string]*packages.Package{dependency.PkgPath: dependency},
				})
			}
			b.ReportAllocs()
			for b.Loop() {
				cache := &Cache{}
				start := make(chan struct{})
				var wg sync.WaitGroup
				for _, pkg := range roots {
					wg.Go(func() {
						<-start
						_, err := cache.packageHash(pkg, HashModeNeedAllDeps)
						assert.NoError(b, err)
					})
				}
				close(start)
				wg.Wait()
			}
		})
	}
}
