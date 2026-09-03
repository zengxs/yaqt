package qtrepo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	yaqtcache "github.com/zengxs/yaqt/internal/cache"
)

func TestCachedClientPersistsRepositoryResources(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		request := requests.Add(1)
		_, _ = fmt.Fprintf(writer, `<a href="qt6_68%d/">release</a>`, request)
	}))
	defer server.Close()

	repository := newCacheTestRepository(t, server.URL)
	ctx := yaqtcache.ContextWithRoot(context.Background(), newRepositoryCacheTestRoot(t))
	first, err := NewCachedClient(server.Client()).ListVersions(ctx, repository)
	if err != nil {
		t.Fatalf("first ListVersions() error = %v", err)
	}
	second, err := NewCachedClient(server.Client()).ListVersions(ctx, repository)
	if err != nil {
		t.Fatalf("second ListVersions() error = %v", err)
	}
	assertVersionStrings(t, first, []string{"6.8.1"})
	assertVersionStrings(t, second, []string{"6.8.1"})
	if got, want := requests.Load(), int32(1); got != want {
		t.Errorf("repository request count = %d, want %d", got, want)
	}
}

func TestCachedClientPersistsPackageMetadata(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writer.Header().Set("Content-Type", "application/xml")
		_, _ = fmt.Fprint(writer, `<Updates>
<PackageUpdate><Name>qt.qt6.681.linux_gcc_64</Name><Version>6.8.1-0-test</Version></PackageUpdate>
</Updates>`)
	}))
	defer server.Close()

	ctx := yaqtcache.ContextWithRoot(context.Background(), newRepositoryCacheTestRoot(t))
	metadataURL := server.URL + "/Updates.xml"
	first, err := NewCachedClient(server.Client()).fetchPackageUpdates(ctx, metadataURL)
	if err != nil {
		t.Fatalf("first fetchPackageUpdates() error = %v", err)
	}
	second, err := NewCachedClient(server.Client()).fetchPackageUpdates(ctx, metadataURL)
	if err != nil {
		t.Fatalf("second fetchPackageUpdates() error = %v", err)
	}
	const packageName = "qt.qt6.681.linux_gcc_64"
	if first[packageName].Version != "6.8.1-0-test" || second[packageName].Version != "6.8.1-0-test" {
		t.Errorf("cached package metadata does not contain %s", packageName)
	}
	if got, want := requests.Load(), int32(1); got != want {
		t.Errorf("package metadata request count = %d, want %d", got, want)
	}
}

func TestCachedClientRefreshesExpiredRepositoryResources(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		request := requests.Add(1)
		_, _ = fmt.Fprintf(writer, `<a href="qt6_68%d/">release</a>`, request)
	}))
	defer server.Close()

	now := time.Date(2026, time.September, 2, 0, 0, 0, 0, time.UTC)
	client := NewCachedClient(server.Client())
	client.resourceCache.now = func() time.Time { return now }
	repository := newCacheTestRepository(t, server.URL)
	ctx := yaqtcache.ContextWithRoot(context.Background(), newRepositoryCacheTestRoot(t))
	first, err := client.ListVersions(ctx, repository)
	if err != nil {
		t.Fatalf("first ListVersions() error = %v", err)
	}
	assertVersionStrings(t, first, []string{"6.8.1"})

	now = now.Add(defaultRepositoryCacheMaxAge + time.Second)
	second, err := client.ListVersions(ctx, repository)
	if err != nil {
		t.Fatalf("second ListVersions() error = %v", err)
	}
	assertVersionStrings(t, second, []string{"6.8.2"})
	if got, want := requests.Load(), int32(2); got != want {
		t.Errorf("repository request count = %d, want %d", got, want)
	}
}

func TestCachedClientReplacesCorruptRepositoryResources(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = fmt.Fprint(writer, `<a href="qt6_681/">release</a>`)
	}))
	defer server.Close()

	cacheRoot := newRepositoryCacheTestRoot(t)
	ctx := yaqtcache.ContextWithRoot(context.Background(), cacheRoot)
	repository := newCacheTestRepository(t, server.URL)
	client := NewCachedClient(server.Client())
	if _, err := client.ListVersions(ctx, repository); err != nil {
		t.Fatalf("first ListVersions() error = %v", err)
	}
	entryPath, _ := repositoryCachePaths(repository.IndexURL())
	absoluteEntryPath := filepath.Join(cacheRoot, entryPath)
	encoded, err := os.ReadFile(absoluteEntryPath)
	if err != nil {
		t.Fatalf("read repository cache entry: %v", err)
	}
	var entry repositoryCacheEntry
	if err := json.Unmarshal(encoded, &entry); err != nil {
		t.Fatalf("decode repository cache entry: %v", err)
	}
	entry.Contents = []byte(`<a href="qt6_682/">tampered release</a>`)
	encoded, err = json.Marshal(entry)
	if err != nil {
		t.Fatalf("encode corrupt repository cache entry: %v", err)
	}
	if err := os.WriteFile(absoluteEntryPath, encoded, 0o600); err != nil {
		t.Fatalf("corrupt repository cache entry: %v", err)
	}

	if _, err := client.ListVersions(ctx, repository); err != nil {
		t.Fatalf("second ListVersions() error = %v", err)
	}
	if _, err := client.ListVersions(ctx, repository); err != nil {
		t.Fatalf("third ListVersions() error = %v", err)
	}
	if got, want := requests.Load(), int32(2); got != want {
		t.Errorf("repository request count = %d, want %d", got, want)
	}
}

func TestCachedClientSeparatesRepositoryMirrors(t *testing.T) {
	var firstRequests atomic.Int32
	firstServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		firstRequests.Add(1)
		_, _ = fmt.Fprint(writer, `<a href="qt6_681/">release</a>`)
	}))
	defer firstServer.Close()
	var secondRequests atomic.Int32
	secondServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		secondRequests.Add(1)
		_, _ = fmt.Fprint(writer, `<a href="qt6_682/">release</a>`)
	}))
	defer secondServer.Close()

	ctx := yaqtcache.ContextWithRoot(context.Background(), newRepositoryCacheTestRoot(t))
	client := NewCachedClient(firstServer.Client())
	firstRepository := newCacheTestRepository(t, firstServer.URL)
	secondRepository := newCacheTestRepository(t, secondServer.URL)
	first, err := client.ListVersions(ctx, firstRepository)
	if err != nil {
		t.Fatalf("first mirror ListVersions() error = %v", err)
	}
	second, err := client.ListVersions(ctx, secondRepository)
	if err != nil {
		t.Fatalf("second mirror ListVersions() error = %v", err)
	}
	assertVersionStrings(t, first, []string{"6.8.1"})
	assertVersionStrings(t, second, []string{"6.8.2"})
	if got, want := firstRequests.Load(), int32(1); got != want {
		t.Errorf("first mirror request count = %d, want %d", got, want)
	}
	if got, want := secondRequests.Load(), int32(1); got != want {
		t.Errorf("second mirror request count = %d, want %d", got, want)
	}
}

func TestCachedClientSerializesConcurrentRepositoryRequests(t *testing.T) {
	firstRequest := make(chan struct{})
	releaseRequest := make(chan struct{})
	var signalFirst sync.Once
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		signalFirst.Do(func() { close(firstRequest) })
		<-releaseRequest
		_, _ = fmt.Fprint(writer, `<a href="qt6_681/">release</a>`)
	}))
	defer server.Close()

	ctx := yaqtcache.ContextWithRoot(context.Background(), newRepositoryCacheTestRoot(t))
	repository := newCacheTestRepository(t, server.URL)
	const consumerCount = 6
	results := make(chan error, consumerCount)
	for range consumerCount {
		go func() {
			_, err := NewCachedClient(server.Client()).ListVersions(ctx, repository)
			results <- err
		}()
	}
	select {
	case <-firstRequest:
	case <-time.After(2 * time.Second):
		close(releaseRequest)
		t.Fatal("repository request did not start")
	}
	time.Sleep(150 * time.Millisecond)
	if got, want := requests.Load(), int32(1); got != want {
		close(releaseRequest)
		for range consumerCount {
			<-results
		}
		t.Fatalf("concurrent repository request count = %d, want %d", got, want)
	}
	close(releaseRequest)
	for range consumerCount {
		if err := <-results; err != nil {
			t.Errorf("ListVersions() error = %v", err)
		}
	}
	if got, want := requests.Load(), int32(1); got != want {
		t.Errorf("repository request count = %d, want %d", got, want)
	}
}

func TestCachedClientDoesNotCacheHTTPFailures(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			http.Error(writer, "unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = fmt.Fprint(writer, `<a href="qt6_681/">release</a>`)
	}))
	defer server.Close()

	ctx := yaqtcache.ContextWithRoot(context.Background(), newRepositoryCacheTestRoot(t))
	repository := newCacheTestRepository(t, server.URL)
	client := NewCachedClient(server.Client())
	if _, err := client.ListVersions(ctx, repository); err == nil ||
		!strings.Contains(err.Error(), "503 Service Unavailable") {
		t.Fatalf("first ListVersions() error = %v, want HTTP status", err)
	}
	if _, err := client.ListVersions(ctx, repository); err != nil {
		t.Fatalf("second ListVersions() error = %v", err)
	}
	if _, err := client.ListVersions(ctx, repository); err != nil {
		t.Fatalf("third ListVersions() error = %v", err)
	}
	if got, want := requests.Load(), int32(2); got != want {
		t.Errorf("repository request count = %d, want %d", got, want)
	}
}

func TestCachedClientDoesNotCacheInvalidRepositoryIndexes(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			_, _ = fmt.Fprint(writer, `repository temporarily unavailable`)
			return
		}
		_, _ = fmt.Fprint(writer, `<a href="qt6_681/">release</a>`)
	}))
	defer server.Close()

	ctx := yaqtcache.ContextWithRoot(context.Background(), newRepositoryCacheTestRoot(t))
	repository := newCacheTestRepository(t, server.URL)
	client := NewCachedClient(server.Client())
	if _, err := client.ListVersions(ctx, repository); err == nil ||
		!strings.Contains(err.Error(), "contains no supported stable versions") {
		t.Fatalf("first ListVersions() error = %v, want invalid repository index", err)
	}
	versions, err := client.ListVersions(ctx, repository)
	if err != nil {
		t.Fatalf("second ListVersions() error = %v", err)
	}
	assertVersionStrings(t, versions, []string{"6.8.1"})
	versions, err = client.ListVersions(ctx, repository)
	if err != nil {
		t.Fatalf("third ListVersions() error = %v", err)
	}
	assertVersionStrings(t, versions, []string{"6.8.1"})
	if got, want := requests.Load(), int32(2); got != want {
		t.Errorf("repository request count = %d, want %d", got, want)
	}
}

func TestCachedClientDoesNotCacheInvalidPackageMetadata(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			_, _ = fmt.Fprint(writer, `<html><body>repository temporarily unavailable</body></html>`)
			return
		}
		_, _ = fmt.Fprint(writer, `<Updates>
<PackageUpdate><Name>qt.qt6.681.linux_gcc_64</Name><Version>6.8.1-0-test</Version></PackageUpdate>
</Updates>`)
	}))
	defer server.Close()

	ctx := yaqtcache.ContextWithRoot(context.Background(), newRepositoryCacheTestRoot(t))
	metadataURL := server.URL + "/Updates.xml"
	client := NewCachedClient(server.Client())
	if _, err := client.fetchPackageUpdates(ctx, metadataURL); err == nil ||
		!strings.Contains(err.Error(), "parse Qt package metadata") {
		t.Fatalf("first fetchPackageUpdates() error = %v, want invalid package metadata", err)
	}
	packages, err := client.fetchPackageUpdates(ctx, metadataURL)
	if err != nil {
		t.Fatalf("second fetchPackageUpdates() error = %v", err)
	}
	const packageName = "qt.qt6.681.linux_gcc_64"
	if packages[packageName].Version != "6.8.1-0-test" {
		t.Errorf("package metadata does not contain %s", packageName)
	}
	if _, err := client.fetchPackageUpdates(ctx, metadataURL); err != nil {
		t.Fatalf("third fetchPackageUpdates() error = %v", err)
	}
	if got, want := requests.Load(), int32(2); got != want {
		t.Errorf("package metadata request count = %d, want %d", got, want)
	}
}

func TestCachedClientRejectsCacheDirectorySymlinkEscape(t *testing.T) {
	for _, directoryName := range []string{"metadata", "locks"} {
		t.Run(directoryName, func(t *testing.T) {
			cacheRoot := newRepositoryCacheTestRoot(t)
			outsideRoot := newRepositoryCacheTestRoot(t)
			if err := os.Symlink(outsideRoot, filepath.Join(cacheRoot, directoryName)); err != nil {
				t.Skipf("create cache directory symlink: %v", err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprint(writer, `<a href="qt6_681/">release</a>`)
			}))
			defer server.Close()

			ctx := yaqtcache.ContextWithRoot(context.Background(), cacheRoot)
			repository := newCacheTestRepository(t, server.URL)
			_, err := NewCachedClient(server.Client()).ListVersions(ctx, repository)
			if err == nil || !strings.Contains(err.Error(), "escapes") {
				t.Fatalf("ListVersions() error = %v, want a cache path escape error", err)
			}
			entries, err := os.ReadDir(outsideRoot)
			if err != nil {
				t.Fatalf("read outside cache directory: %v", err)
			}
			if len(entries) != 0 {
				t.Errorf("outside cache directory contains %d entries, want none", len(entries))
			}
		})
	}
}

func newCacheTestRepository(t *testing.T, baseURL string) Repository {
	t.Helper()
	repository, err := NewRepository(baseURL, HostLinux, TargetDesktop)
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	return repository
}

func newRepositoryCacheTestRoot(t *testing.T) string {
	t.Helper()
	repositoryRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	parent := filepath.Join(repositoryRoot, ".tmp", "repository-cache-tests")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatalf("create repository cache test parent: %v", err)
	}
	directory, err := os.MkdirTemp(parent, "cache-*")
	if err != nil {
		t.Fatalf("create repository cache test root: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(directory); err != nil {
			t.Errorf("remove repository cache test root: %v", err)
		}
		_ = os.Remove(parent)
	})
	return directory
}
