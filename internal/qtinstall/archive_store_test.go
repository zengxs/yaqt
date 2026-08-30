package qtinstall

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/zengxs/yaqt/internal/buildinfo"
	"github.com/zengxs/yaqt/internal/qtrepo"
)

func TestArchiveStoreFetchesAndReusesVerifiedArchive(t *testing.T) {
	contents := []byte("verified Qt archive")
	digest := sha256.Sum256(contents)
	var archiveRequests atomic.Int32
	var checksumRequests atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/qtbase.7z.sha256":
			checksumRequests.Add(1)
			if got, want := request.Header.Get("User-Agent"), buildinfo.UserAgent; got != want {
				t.Errorf("checksum User-Agent = %q, want %q", got, want)
			}
			_, _ = fmt.Fprintf(writer, "%x  qtbase.7z\n", digest)
		case "/qtbase.7z":
			archiveRequests.Add(1)
			if got, want := request.Header.Get("User-Agent"), buildinfo.UserAgent; got != want {
				t.Errorf("archive User-Agent = %q, want %q", got, want)
			}
			_, _ = writer.Write(contents)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	cacheDir := newTestCacheDir(t)
	store, err := NewArchiveStore(server.Client(), cacheDir)
	if err != nil {
		t.Fatalf("NewArchiveStore() error = %v", err)
	}
	archive := qtrepo.Archive{
		Name: "qtbase",
		URL:  server.URL + "/qtbase.7z",
		Checksum: qtrepo.Checksum{
			Algorithm: qtrepo.ChecksumSHA256,
			URL:       server.URL + "/qtbase.7z.sha256",
		},
	}

	path, err := store.Fetch(context.Background(), archive)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	wantPath := filepath.Join(cacheDir, "downloads", "sha256", fmt.Sprintf("%x.7z", digest))
	if path != wantPath {
		t.Errorf("Fetch() path = %q, want %q", path, wantPath)
	}
	gotContents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read cached archive: %v", err)
	}
	if string(gotContents) != string(contents) {
		t.Errorf("cached archive = %q, want %q", gotContents, contents)
	}

	secondPath, err := store.Fetch(context.Background(), archive)
	if err != nil {
		t.Fatalf("second Fetch() error = %v", err)
	}
	if secondPath != path {
		t.Errorf("second Fetch() path = %q, want %q", secondPath, path)
	}
	if got, want := checksumRequests.Load(), int32(2); got != want {
		t.Errorf("checksum request count = %d, want %d", got, want)
	}
	if got, want := archiveRequests.Load(), int32(1); got != want {
		t.Errorf("archive request count = %d, want %d", got, want)
	}
	assertNoPartialFiles(t, filepath.Dir(path))
}

func TestArchiveStoreReplacesCorruptCachedArchive(t *testing.T) {
	contents := []byte("valid archive")
	digest := sha256.Sum256(contents)
	var archiveRequests atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/archive.7z.sha256":
			_, _ = fmt.Fprintf(writer, "%x\n", digest)
		case "/archive.7z":
			archiveRequests.Add(1)
			_, _ = writer.Write(contents)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	store, err := NewArchiveStore(server.Client(), newTestCacheDir(t))
	if err != nil {
		t.Fatalf("NewArchiveStore() error = %v", err)
	}
	archive := qtrepo.Archive{
		URL: server.URL + "/archive.7z",
		Checksum: qtrepo.Checksum{
			Algorithm: qtrepo.ChecksumSHA256,
			URL:       server.URL + "/archive.7z.sha256",
		},
	}
	path, err := store.Fetch(context.Background(), archive)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if err := os.WriteFile(path, []byte("corrupt"), 0o600); err != nil {
		t.Fatalf("corrupt cached archive: %v", err)
	}

	path, err = store.Fetch(context.Background(), archive)
	if err != nil {
		t.Fatalf("Fetch() after corruption error = %v", err)
	}
	gotContents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read replacement archive: %v", err)
	}
	if string(gotContents) != string(contents) {
		t.Errorf("replacement archive = %q, want %q", gotContents, contents)
	}
	if got, want := archiveRequests.Load(), int32(2); got != want {
		t.Errorf("archive request count = %d, want %d", got, want)
	}
}

func TestArchiveStoreRejectsChecksumMismatchAndRemovesPartialFile(t *testing.T) {
	expected := sha256.Sum256([]byte("expected archive"))
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/archive.7z.sha256":
			_, _ = fmt.Fprintf(writer, "%x\n", expected)
		case "/archive.7z":
			_, _ = writer.Write([]byte("different archive"))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	cacheDir := newTestCacheDir(t)
	store, err := NewArchiveStore(server.Client(), cacheDir)
	if err != nil {
		t.Fatalf("NewArchiveStore() error = %v", err)
	}
	_, err = store.Fetch(context.Background(), qtrepo.Archive{
		URL: server.URL + "/archive.7z",
		Checksum: qtrepo.Checksum{
			Algorithm: qtrepo.ChecksumSHA256,
			URL:       server.URL + "/archive.7z.sha256",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") {
		t.Fatalf("Fetch() error = %v, want a SHA-256 mismatch", err)
	}

	downloadDir := filepath.Join(cacheDir, "downloads", "sha256")
	entries, err := os.ReadDir(downloadDir)
	if err != nil {
		t.Fatalf("read download directory: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("download directory contains %v after checksum failure, want empty", entries)
	}
}

func TestArchiveStoreRejectsInvalidChecksumBeforeDownloadingArchive(t *testing.T) {
	var archiveRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/archive.7z.sha256":
			_, _ = writer.Write([]byte("not-a-sha256"))
		case "/archive.7z":
			archiveRequests.Add(1)
			_, _ = writer.Write([]byte("archive"))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	store, err := NewArchiveStore(server.Client(), newTestCacheDir(t))
	if err != nil {
		t.Fatalf("NewArchiveStore() error = %v", err)
	}
	_, err = store.Fetch(context.Background(), qtrepo.Archive{
		URL: server.URL + "/archive.7z",
		Checksum: qtrepo.Checksum{
			Algorithm: qtrepo.ChecksumSHA256,
			URL:       server.URL + "/archive.7z.sha256",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid SHA-256") {
		t.Fatalf("Fetch() error = %v, want an invalid SHA-256 error", err)
	}
	if got := archiveRequests.Load(); got != 0 {
		t.Errorf("archive request count = %d, want 0", got)
	}
}

func TestArchiveStoreDoesNotFallBackWhenSHA256IsMissing(t *testing.T) {
	var fallbackRequests atomic.Int32
	var archiveRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/archive.7z.sha256":
			http.NotFound(writer, request)
		case "/archive.7z.sha1":
			fallbackRequests.Add(1)
			_, _ = writer.Write([]byte("unused"))
		case "/archive.7z":
			archiveRequests.Add(1)
			_, _ = writer.Write([]byte("archive"))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	store, err := NewArchiveStore(server.Client(), newTestCacheDir(t))
	if err != nil {
		t.Fatalf("NewArchiveStore() error = %v", err)
	}
	_, err = store.Fetch(context.Background(), qtrepo.Archive{
		URL: server.URL + "/archive.7z",
		Checksum: qtrepo.Checksum{
			Algorithm: qtrepo.ChecksumSHA256,
			URL:       server.URL + "/archive.7z.sha256",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "404 Not Found") {
		t.Fatalf("Fetch() error = %v, want a missing SHA-256 error", err)
	}
	if got := fallbackRequests.Load(); got != 0 {
		t.Errorf("SHA-1 fallback request count = %d, want 0", got)
	}
	if got := archiveRequests.Load(); got != 0 {
		t.Errorf("archive request count = %d, want 0", got)
	}
}

func TestArchiveStoreRejectsUnsupportedChecksumAlgorithmBeforeRequest(t *testing.T) {
	store, err := NewArchiveStore(nil, newTestCacheDir(t))
	if err != nil {
		t.Fatalf("NewArchiveStore() error = %v", err)
	}
	_, err = store.Fetch(context.Background(), qtrepo.Archive{
		URL: "https://mirror.example/archive.7z",
		Checksum: qtrepo.Checksum{
			Algorithm: "sha1",
			URL:       "https://mirror.example/archive.7z.sha1",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported checksum algorithm") {
		t.Fatalf("Fetch() error = %v, want an unsupported checksum algorithm error", err)
	}
}

func TestResolveCacheDirPrecedence(t *testing.T) {
	environmentDir := filepath.Join(newTestCacheDir(t), "environment")
	t.Setenv(cacheDirEnvironmentVariable, environmentDir)

	explicitDir := filepath.Join(newTestCacheDir(t), "explicit")
	got, err := ResolveCacheDir(explicitDir)
	if err != nil {
		t.Fatalf("ResolveCacheDir(explicit) error = %v", err)
	}
	if got != filepath.Clean(explicitDir) {
		t.Errorf("ResolveCacheDir(explicit) = %q, want %q", got, filepath.Clean(explicitDir))
	}

	got, err = ResolveCacheDir("")
	if err != nil {
		t.Fatalf("ResolveCacheDir(environment) error = %v", err)
	}
	if got != filepath.Clean(environmentDir) {
		t.Errorf("ResolveCacheDir(environment) = %q, want %q", got, filepath.Clean(environmentDir))
	}
}

func TestResolveCacheDirUsesOperatingSystemCache(t *testing.T) {
	t.Setenv(cacheDirEnvironmentVariable, "")
	wantRoot, err := os.UserCacheDir()
	if err != nil {
		t.Fatalf("os.UserCacheDir() error = %v", err)
	}

	got, err := ResolveCacheDir("")
	if err != nil {
		t.Fatalf("ResolveCacheDir() error = %v", err)
	}
	if want := filepath.Join(wantRoot, "yaqt"); got != want {
		t.Errorf("ResolveCacheDir() = %q, want %q", got, want)
	}
}

func assertNoPartialFiles(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read download directory: %v", err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".part") {
			t.Errorf("partial file %q remains after Fetch()", entry.Name())
		}
	}
}

func newTestCacheDir(t *testing.T) string {
	t.Helper()
	repositoryRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	parent := filepath.Join(repositoryRoot, ".tmp", "yaqt")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatalf("create test cache parent: %v", err)
	}
	directory, err := os.MkdirTemp(parent, "archive-store-*")
	if err != nil {
		t.Fatalf("create test cache directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(directory); err != nil {
			t.Errorf("remove test cache directory: %v", err)
		}
		_ = os.Remove(parent)
	})
	return directory
}
