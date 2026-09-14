package processors

import (
	"fmt"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"

	"github.com/golangci/golangci-lint/v2/pkg/logutils"
	"github.com/golangci/golangci-lint/v2/pkg/result"
)

func TestFilenameUnadjusterInvalidFiles(t *testing.T) {
	for _, source := range []string{
		"package p\nvar Broken =\n",
		"//line source.tmpl:20\npackage p\nvar Broken =\n",
		"//line source.tmpl:0\npackage p\n",
		"/*line source.tmpl:20\npackage p\n",
	} {
		t.Run(source, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), "invalid.go")
			require.NoError(t, os.WriteFile(filename, []byte(source), 0o600))
			p := NewFilenameUnadjuster([]*packages.Package{{CompiledGoFiles: []string{filename}}}, logutils.NewStderrLog("test"))
			assert.Empty(t, p.m)
		})
	}
}

func TestFilenameUnadjusterMissingFile(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "missing.go")
	p := NewFilenameUnadjuster([]*packages.Package{{CompiledGoFiles: []string{filename}}}, logutils.NewStderrLog("test"))
	assert.Empty(t, p.m)
}

func TestFilenameUnadjusterNonGoFile(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "cgo-cache-d")
	require.NoError(t, os.WriteFile(filename, []byte("//line original.go:1\npackage p\n"), 0o600))
	p := NewFilenameUnadjuster([]*packages.Package{{CompiledGoFiles: []string{filename}}}, logutils.NewStderrLog("test"))
	assert.Empty(t, p.m)
}

func TestFilenameUnadjusterMixedFiles(t *testing.T) {
	dir := t.TempDir()
	ordinary := filepath.Join(dir, "ordinary.go")
	require.NoError(t, os.WriteFile(ordinary, []byte("package p\nvar Ordinary = 1\n"), 0o600))
	generated := filepath.Join(dir, "generated.go")
	source := "//line source.tmpl:20:3\npackage p\nvar Value = 1\n"
	require.NoError(t, os.WriteFile(generated, []byte(source), 0o600))
	p := NewFilenameUnadjuster([]*packages.Package{{CompiledGoFiles: []string{ordinary, generated}}}, logutils.NewStderrLog("test"))
	offset := strings.Index(source, "Value")
	input := &result.Issue{Pos: token.Position{Filename: filepath.Join(dir, "source.tmpl"), Offset: offset, Line: 21}}
	output, err := p.Process([]*result.Issue{input})
	require.NoError(t, err)
	require.Len(t, output, 1)
	assert.Equal(t, token.Position{Filename: generated, Offset: offset, Line: 3, Column: 5}, output[0].Pos)
}

func TestFilenameUnadjusterEmptyPackages(t *testing.T) {
	p := NewFilenameUnadjuster(nil, logutils.NewStderrLog("test"))
	assert.Empty(t, p.m)
}

func TestFilenameUnadjusterAllChunks(t *testing.T) {
	previous := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(previous)

	dir := t.TempDir()
	var pkgs []*packages.Package
	var issues []*result.Issue
	var positions []token.Position
	for i := range 2001 {
		filename := filepath.Join(dir, fmt.Sprintf("generated%d.go", i))
		adjusted := filepath.Join(dir, fmt.Sprintf("source%d.tmpl", i))
		source := fmt.Sprintf("//line source%d.tmpl:20:3\npackage p\nvar Value = 1\n", i)
		require.NoError(t, os.WriteFile(filename, []byte(source), 0o600))
		pkgs = append(pkgs, &packages.Package{CompiledGoFiles: []string{filename}})
		offset := strings.Index(source, "Value")
		issues = append(issues, &result.Issue{Pos: token.Position{Filename: adjusted, Offset: offset, Line: 21}})
		positions = append(positions, token.Position{Filename: filename, Offset: offset, Line: 3, Column: 5})
	}
	p := NewFilenameUnadjuster(pkgs, logutils.NewStderrLog("test"))
	require.Len(t, p.m, len(pkgs))
	output, err := p.Process(issues)
	require.NoError(t, err)
	require.Len(t, output, len(positions))
	for i, issue := range output {
		assert.Equal(t, positions[i], issue.Pos)
	}
}

func TestFilenameUnadjusterDuplicatePackages(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "generated.go")
	source := "//line source.tmpl:20\npackage p\nvar Value = 1\n"
	require.NoError(t, os.WriteFile(filename, []byte(source), 0o600))
	var pkgs []*packages.Package
	for range 32 {
		pkgs = append(pkgs, &packages.Package{CompiledGoFiles: []string{filename}})
	}
	p := NewFilenameUnadjuster(pkgs, logutils.NewStderrLog("test"))
	assert.Len(t, p.m, 1)
}

func BenchmarkFilenameUnadjusterPackages(b *testing.B) {
	for _, count := range []int{1, 64, 256} {
		b.Run(fmt.Sprintf("packages=%d", count), func(b *testing.B) {
			dir := b.TempDir()
			var source strings.Builder
			source.WriteString("package p\n")
			for i := range 100 {
				fmt.Fprintf(&source, "func F%d() int { return %d }\n", i, i)
			}
			var pkgs []*packages.Package
			for i := range count {
				filename := filepath.Join(dir, fmt.Sprintf("source%d.go", i))
				require.NoError(b, os.WriteFile(filename, []byte(source.String()), 0o600))
				pkgs = append(pkgs, &packages.Package{CompiledGoFiles: []string{filename}})
			}
			logger := logutils.NewStderrLog("test")
			b.ReportAllocs()
			for b.Loop() {
				p := NewFilenameUnadjuster(pkgs, logger)
				require.Empty(b, p.m)
			}
		})
	}
}
