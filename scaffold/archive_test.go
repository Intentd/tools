package scaffold

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractTarGz(t *testing.T) {
	archive := makeTarGz(t, []archiveEntry{
		{name: "example/", kind: tar.TypeDir},
		{name: "example/main.go", content: "package main\n"},
		{name: "example/scripts/run.sh", content: "#!/bin/sh\necho hello\n"},
	})

	target := filepath.Join(t.TempDir(), "output")
	if err := ExtractTarGz(target, bytes.NewReader(archive)); err != nil {
		t.Fatal(err)
	}

	assertFile(t, filepath.Join(target, "example/main.go"), "package main\n")
	assertFile(t, filepath.Join(target, "example/scripts/run.sh"), "#!/bin/sh\necho hello\n")
	assertMode(t, filepath.Join(target, "example/scripts/run.sh"), 0755)
}

func TestExtractTarGzOverwritesExistingFiles(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "main.go"), []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}

	archive := makeTarGz(t, []archiveEntry{
		{name: "main.go", content: "new"},
	})

	if err := ExtractTarGz(target, bytes.NewReader(archive)); err != nil {
		t.Fatal(err)
	}

	assertFile(t, filepath.Join(target, "main.go"), "new")
}

func TestExtractTarGzRejectsUnsafePaths(t *testing.T) {
	tests := []string{
		"../outside.txt",
		"nested/../../outside.txt",
		"/absolute.txt",
		`nested\outside.txt`,
	}

	for _, name := range tests {
		t.Run(name, func(t *testing.T) {
			archive := makeTarGz(t, []archiveEntry{
				{name: name, content: "bad"},
			})

			err := ExtractTarGz(t.TempDir(), bytes.NewReader(archive))
			if err == nil {
				t.Fatalf("expected unsafe path %q to fail", name)
			}
		})
	}
}

func TestExtractTarGzRejectsUnsupportedEntries(t *testing.T) {
	archive := makeTarGz(t, []archiveEntry{
		{name: "link", kind: tar.TypeSymlink, linkname: "target"},
	})

	if err := ExtractTarGz(t.TempDir(), bytes.NewReader(archive)); err == nil {
		t.Fatal("expected symlink entry to be rejected")
	}
}

func TestExtractTarGzRejectsInvalidArchive(t *testing.T) {
	err := ExtractTarGz(t.TempDir(), strings.NewReader("not a gzip archive"))
	if err == nil {
		t.Fatal("expected invalid archive to fail")
	}
}

func TestExtractTarGzNilReader(t *testing.T) {
	if err := ExtractTarGz(t.TempDir(), nil); err == nil {
		t.Fatal("expected nil reader to fail")
	}
}

func TestSafeArchivePath(t *testing.T) {
	tests := []struct {
		name    string
		want    string
		wantErr bool
	}{
		{name: "file.txt", want: "file.txt"},
		{name: "nested/file.txt", want: "nested/file.txt"},
		{name: "nested/./file.txt", want: "nested/file.txt"},
		{name: ".", want: "."},
		{name: "../outside", wantErr: true},
		{name: "nested/../../outside", wantErr: true},
		{name: "/outside", wantErr: true},
		{name: `nested\file`, wantErr: true},
		{name: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := safeArchivePath(tt.name)
			if (err != nil) != tt.wantErr {
				t.Fatalf("safeArchivePath(%q) error = %v, wantErr %v", tt.name, err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("safeArchivePath(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestDownloadPackDownloadsAndCaches(t *testing.T) {
	var requests int

	server := newPackServer(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		assertRequestPath(t, r, "/releases/download/v1.2.3/examples_demo_v1.2.3.tar.gz")
		_, _ = w.Write(makeTarGz(t, []archiveEntry{
			{name: "README.md", content: "hello"},
		}))
	})
	defer server.Close()

	options := testDownloadOptions(server, t)

	releaseURL := testReleaseURL(server, options.Version)

	archivePath, err := downloadPackFromURL("demo", options, releaseURL)

	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("cached archive does not exist: %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}

	// A second call should reuse the cached archive.
	secondPath, err := downloadPackFromURL("demo", options, releaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if secondPath != archivePath {
		t.Fatalf("second path = %q, want %q", secondPath, archivePath)
	}
	if requests != 1 {
		t.Fatalf("cached call made another request: requests = %d", requests)
	}
}

func TestDownloadPackRefreshesCache(t *testing.T) {
	var requests int

	server := newPackServer(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = w.Write(makeTarGz(t, []archiveEntry{
			{name: "version.txt", content: fmt.Sprintf("download-%d", requests)},
		}))
	})
	defer server.Close()

	options := testDownloadOptions(server, t)

	firstPath, err := downloadPackFromURL("demo", options, server.URL)
	if err != nil {
		t.Fatal(err)
	}

	options.Refresh = true

	secondPath, err := downloadPackFromURL("demo", options, server.URL)
	if err != nil {
		t.Fatal(err)
	}

	if firstPath != secondPath {
		t.Fatalf("archive path changed: got %q, want %q", secondPath, firstPath)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}

	root := t.TempDir()
	if err := ExtractPackArchive(root, "demo", secondPath); err != nil {
		t.Fatal(err)
	}

	assertFile(t, filepath.Join(root, "demo", "version.txt"), "download-2")
}

func TestDownloadPackFailedRefreshPreservesCache(t *testing.T) {
	var requests int

	server := newPackServer(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			_, _ = w.Write(makeTarGz(t, []archiveEntry{
				{name: "version.txt", content: "original"},
			}))
			return
		}
		http.Error(w, "unavailable", http.StatusInternalServerError)
	})
	defer server.Close()

	options := testDownloadOptions(server, t)

	archivePath, err := downloadPackFromURL("demo", options, server.URL)
	if err != nil {
		t.Fatal(err)
	}

	options.Refresh = true
	if _, err := downloadPackFromURL("demo", options, server.URL); err == nil {
		t.Fatal("expected refresh to fail")
	}

	root := t.TempDir()
	if err := ExtractPackArchive(root, "demo", archivePath); err != nil {
		t.Fatal(err)
	}

	assertFile(t, filepath.Join(root, "demo", "version.txt"), "original")
}

func TestDownloadPackRejectsBadStatus(t *testing.T) {
	server := newPackServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	defer server.Close()

	_, err := downloadPackFromURL("demo", testDownloadOptions(server, t), server.URL)
	if err == nil {
		t.Fatal("expected HTTP error")
	}
}

func TestDownloadPackValidation(t *testing.T) {
	tests := []struct {
		name    string
		pack    string
		options DownloadOptions
	}{
		{name: "missing pack", options: validDownloadOptions(t)},
		{name: "dot pack", pack: ".", options: validDownloadOptions(t)},
		{name: "traversal pack", pack: "../demo", options: validDownloadOptions(t)},
		{name: "nested pack", pack: "nested/demo", options: validDownloadOptions(t)},
		{name: "missing repository", pack: "demo", options: DownloadOptions{Version: "v1", CacheDir: t.TempDir()}},
		{name: "missing version", pack: "demo", options: DownloadOptions{Repository: "owner/repo", CacheDir: t.TempDir()}},
		{name: "missing cache", pack: "demo", options: DownloadOptions{Repository: "owner/repo", Version: "v1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := downloadPackFromURL(tt.pack, tt.options, "https://example.com")
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestExtractPackArchive(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(t.TempDir(), "examples_demo_v1.tar.gz")

	archive := makeTarGz(t, []archiveEntry{
		{name: "README.md", content: "hello"},
	})
	if err := os.WriteFile(archivePath, archive, 0644); err != nil {
		t.Fatal(err)
	}

	if err := ExtractPackArchive(root, "demo", archivePath); err != nil {
		t.Fatal(err)
	}

	assertFile(t, filepath.Join(root, "demo", "README.md"), "hello")
}

func TestExtractPackArchiveValidation(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(t.TempDir(), "archive.tar.gz")
	if err := os.WriteFile(archivePath, []byte("not an archive"), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		root        string
		pack        string
		archivePath string
	}{
		{name: "missing root", pack: "demo", archivePath: archivePath},
		{name: "missing pack", root: root, archivePath: archivePath},
		{name: "dot pack", root: root, pack: ".", archivePath: archivePath},
		{name: "traversal pack", root: root, pack: "../demo", archivePath: archivePath},
		{name: "missing archive", root: root, pack: "demo"},
		{name: "nonexistent archive", root: root, pack: "demo", archivePath: filepath.Join(t.TempDir(), "missing.tar.gz")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ExtractPackArchive(tt.root, tt.pack, tt.archivePath); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestDownloadAndExtract(t *testing.T) {
	server := newPackServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(makeTarGz(t, []archiveEntry{
			{name: "README.md", content: "hello"},
		}))
	})
	defer server.Close()

	root := t.TempDir()
	options := testDownloadOptions(server, t)

	archivePath, err := downloadPackFromURL("demo", options, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := ExtractPackArchive(root, "demo", archivePath); err != nil {
		t.Fatal(err)
	}

	assertFile(t, filepath.Join(root, "demo", "README.md"), "hello")
}

func TestClearCache(t *testing.T) {
	cache := t.TempDir()

	if err := os.WriteFile(filepath.Join(cache, "one"), []byte("1"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cache, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "nested", "two"), []byte("2"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := ClearCache(cache); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(cache)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("cache still contains entries: %v", entries)
	}
}

func TestClearCacheMissingDirectory(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "missing")
	if err := ClearCache(cache); err != nil {
		t.Fatalf("ClearCache on missing directory: %v", err)
	}
}

func TestClearCacheRejectsFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := ClearCache(file); err == nil {
		t.Fatal("expected error for file path")
	}
}

type archiveEntry struct {
	name     string
	content  string
	kind     byte
	linkname string
}

func makeTarGz(t *testing.T, entries []archiveEntry) []byte {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for _, entry := range entries {
		kind := entry.kind
		if kind == 0 {
			kind = tar.TypeReg
		}

		header := &tar.Header{
			Name:     entry.name,
			Typeflag: kind,
			Mode:     0644,
			Linkname: entry.linkname,
			Size:     int64(len(entry.content)),
		}
		if kind == tar.TypeDir {
			header.Mode = 0755
			header.Size = 0
		}

		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if kind == tar.TypeReg {
			if _, err := io.WriteString(tw, entry.content); err != nil {
				t.Fatal(err)
			}
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("file %s = %q, want %q", path, got, want)
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != want {
		t.Fatalf("mode for %s = %o, want %o", path, info.Mode().Perm(), want)
	}
}

func validDownloadOptions(t *testing.T) DownloadOptions {
	t.Helper()
	return DownloadOptions{
		Repository: "owner/repo",
		Version:    "v1",
		CacheDir:   t.TempDir(),
	}
}

func testDownloadOptions(server *httptest.Server, t *testing.T) DownloadOptions {
	t.Helper()
	return DownloadOptions{
		Repository: "owner/repo",
		Version:    "v1.2.3",
		CacheDir:   filepath.Join(t.TempDir(), "cache"),
		HTTPClient: server.Client(),
	}
}

func newPackServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(handler)
}

func assertRequestPath(t *testing.T, r *http.Request, want string) {
	t.Helper()
	if r.URL.Path != want {
		t.Errorf("request path = %q, want %q", r.URL.Path, want)
	}
}

func testReleaseURL(server *httptest.Server, version string) string {
	return server.URL + "/releases/download/" + version
}
