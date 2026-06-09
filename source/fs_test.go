package source

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --------------- fstest.MapFS ---------------

func TestFSSource_Load_FromMapFS(t *testing.T) {
	fsys := fstest.MapFS{
		"scripts/hello.lua": &fstest.MapFile{
			Data: []byte("print('hello')"),
		},
	}

	src, err := NewFileSystemSource(fsys)
	require.NoError(t, err)
	defer func() { _ = src.Close() }()

	got, err := src.Load(context.Background(), "scripts/hello.lua")
	require.NoError(t, err)
	assert.Equal(t, "print('hello')", got)
}

func TestFSSource_Load_WithPrefix(t *testing.T) {
	fsys := fstest.MapFS{
		"lua/main.lua": &fstest.MapFile{
			Data: []byte("print('main')"),
		},
	}

	src, err := NewFileSystemSource(fsys, WithFSPrefix("lua"))
	require.NoError(t, err)
	defer func() { _ = src.Close() }()

	got, err := src.Load(context.Background(), "main.lua")
	require.NoError(t, err)
	assert.Equal(t, "print('main')", got)
}

func TestFSSource_Load_WithPrefix_StripsLeadingSlash(t *testing.T) {
	fsys := fstest.MapFS{
		"scripts/hello.lua": &fstest.MapFile{
			Data: []byte("print('hello')"),
		},
	}

	// Prefix with leading slash should be normalized.
	src, err := NewFileSystemSource(fsys, WithFSPrefix("/scripts/"))
	require.NoError(t, err)
	defer func() { _ = src.Close() }()

	// Key with leading slash should be normalized.
	got, err := src.Load(context.Background(), "/hello.lua")
	require.NoError(t, err)
	assert.Equal(t, "print('hello')", got)
}

func TestFSSource_Load_NotFound(t *testing.T) {
	fsys := fstest.MapFS{
		"exists.lua": &fstest.MapFile{Data: []byte("x")},
	}

	src, err := NewFileSystemSource(fsys)
	require.NoError(t, err)
	defer func() { _ = src.Close() }()

	_, err = src.Load(context.Background(), "missing.lua")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fs source: read")
}

func TestFSSource_Load_CancelledContext(t *testing.T) {
	fsys := fstest.MapFS{
		"a.lua": &fstest.MapFile{Data: []byte("x")},
	}

	src, err := NewFileSystemSource(fsys)
	require.NoError(t, err)
	defer func() { _ = src.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = src.Load(ctx, "a.lua")
	require.ErrorIs(t, err, context.Canceled)
}

func TestFSSource_New_NilFS(t *testing.T) {
	_, err := NewFileSystemSource(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

func TestFSSource_Close_NoOp(t *testing.T) {
	fsys := fstest.MapFS{"a.lua": &fstest.MapFile{Data: []byte("x")}}
	src, err := NewFileSystemSource(fsys)
	require.NoError(t, err)

	assert.NoError(t, src.Close())
	// Idempotent.
	assert.NoError(t, src.Close())
}

func TestFSSource_ImplementsReader(t *testing.T) {
	// Compile-time assertion is in fs.go; this is a runtime sanity check.
	fsys := fstest.MapFS{"a.lua": &fstest.MapFile{Data: []byte("x")}}
	src, err := NewFileSystemSource(fsys)
	require.NoError(t, err)

	var _ Reader = src
}

// --------------- os.DirFS ---------------

func TestFSSource_Load_FromDirFS(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lua")
	require.NoError(t, os.WriteFile(path, []byte("print('dir')"), 0o644))

	src, err := NewFileSystemSource(os.DirFS(dir))
	require.NoError(t, err)
	defer func() { _ = src.Close() }()

	got, err := src.Load(context.Background(), "test.lua")
	require.NoError(t, err)
	assert.Equal(t, "print('dir')", got)
}

func TestFSSource_Load_FromDirFS_WithPrefix(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "scripts")
	require.NoError(t, os.Mkdir(subDir, 0o755))
	path := filepath.Join(subDir, "hello.lua")
	require.NoError(t, os.WriteFile(path, []byte("print('prefix')"), 0o644))

	src, err := NewFileSystemSource(os.DirFS(dir), WithFSPrefix("scripts"))
	require.NoError(t, err)
	defer func() { _ = src.Close() }()

	got, err := src.Load(context.Background(), "hello.lua")
	require.NoError(t, err)
	assert.Equal(t, "print('prefix')", got)
}

// --------------- archive/zip ---------------

// helper: create a zip archive in memory and return fs.FS.
func newZipFS(t *testing.T, files map[string]string) *zip.Reader {
	t.Helper()
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)
	for name, content := range files {
		fw, err := w.Create(name)
		require.NoError(t, err)
		_, err = fw.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)
	return zr
}

func TestFSSource_Load_FromZipArchive(t *testing.T) {
	fsys := newZipFS(t, map[string]string{
		"main.lua":      "print('zip main')",
		"lib/utils.lua": "print('zip utils')",
	})

	src, err := NewFileSystemSource(fsys)
	require.NoError(t, err)
	defer func() { _ = src.Close() }()

	got, err := src.Load(context.Background(), "main.lua")
	require.NoError(t, err)
	assert.Equal(t, "print('zip main')", got)

	got, err = src.Load(context.Background(), "lib/utils.lua")
	require.NoError(t, err)
	assert.Equal(t, "print('zip utils')", got)
}

func TestFSSource_Load_FromZipArchive_WithPrefix(t *testing.T) {
	fsys := newZipFS(t, map[string]string{
		"lua/hello.lua": "print('zip hello')",
	})

	src, err := NewFileSystemSource(fsys, WithFSPrefix("lua"))
	require.NoError(t, err)
	defer func() { _ = src.Close() }()

	got, err := src.Load(context.Background(), "hello.lua")
	require.NoError(t, err)
	assert.Equal(t, "print('zip hello')", got)
}

// --------------- Prefix Normalization ---------------

func TestFSSource_Prefix_Normalization(t *testing.T) {
	tests := []struct {
		prefix  string
		key     string
		wantKey string
	}{
		{"", "main.lua", "main.lua"},
		{"scripts", "main.lua", "scripts/main.lua"},
		{"scripts/", "main.lua", "scripts/main.lua"},
		{"/scripts", "main.lua", "scripts/main.lua"},
		{"/scripts/", "main.lua", "scripts/main.lua"},
		{"scripts", "/main.lua", "scripts/main.lua"},
	}
	for _, tc := range tests {
		t.Run(tc.prefix+"+"+tc.key, func(t *testing.T) {
			fsys := fstest.MapFS{
				tc.wantKey: &fstest.MapFile{Data: []byte("ok")},
			}
			src, err := NewFileSystemSource(fsys, WithFSPrefix(tc.prefix))
			require.NoError(t, err)
			defer func() { _ = src.Close() }()

			got, err := src.Load(context.Background(), tc.key)
			require.NoError(t, err)
			assert.Equal(t, "ok", got)
		})
	}
}

// --------------- Concurrency ---------------

func TestFSSource_Load_Concurrent(t *testing.T) {
	fsys := fstest.MapFS{
		"a.lua": &fstest.MapFile{Data: []byte("print('a')")},
		"b.lua": &fstest.MapFile{Data: []byte("print('b')")},
		"c.lua": &fstest.MapFile{Data: []byte("print('c')")},
	}

	src, err := NewFileSystemSource(fsys)
	require.NoError(t, err)
	defer func() { _ = src.Close() }()

	const goroutines = 30
	const loops = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < loops; j++ {
				key := string([]byte{'a' + byte(j%3)}) + ".lua"
				code, err := src.Load(context.Background(), key)
				require.NoError(t, err)
				require.True(t, strings.Contains(code, "print"))
			}
		}()
	}
	wg.Wait()
}
