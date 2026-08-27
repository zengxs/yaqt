package qtrepo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestParseVersionsFiltersDeduplicatesAndSorts(t *testing.T) {
	index, err := os.Open("testdata/desktop-index.html")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer index.Close()

	repository, err := NewRepository(DefaultBaseURL, HostLinux, TargetDesktop)
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	versions, err := parseVersions(index, repository)
	if err != nil {
		t.Fatalf("parseVersions() error = %v", err)
	}

	want := []string{"6.8.0", "6.9.3", "6.10.3"}
	assertVersionStrings(t, versions, want)
}

func TestParseVersionsAcceptsCurrentAllOSQtExtensions(t *testing.T) {
	repository, err := NewRepository(DefaultBaseURL, HostAllOS, TargetQt)
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	index := strings.NewReader(`
		<a href="qt6_673_src_doc_examples/">unsupported old release</a>
		<a href="qt6_680_unix_line_endings_src/">Unix sources</a>
		<a href="qt6_680_windows_line_endings_src/">Windows sources</a>
		<a href="qt6_680_src_doc_examples/">documentation</a>
		<a href="qt6_680_preview/">preview</a>
		<a href="qt6_693_unix_line_endings_src/">6.9.3 sources</a>
	`)

	versions, err := parseVersions(index, repository)
	if err != nil {
		t.Fatalf("parseVersions() error = %v", err)
	}
	assertVersionStrings(t, versions, []string{"6.8.0", "6.9.3"})
}

func TestClientListVersions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		wantPath := "/mirror/online/qtsdkrepository/linux_x64/desktop/"
		if request.URL.Path != wantPath {
			t.Errorf("request path = %q, want %q", request.URL.Path, wantPath)
		}
		if got := request.Header.Get("User-Agent"); got != "yaqt/0.1.0" {
			t.Errorf("User-Agent = %q, want %q", got, "yaqt/0.1.0")
		}
		_, _ = writer.Write([]byte(`<a href="qt6_681/">qt6_681</a>`))
	}))
	defer server.Close()

	repository, err := NewRepository(server.URL+"/mirror", HostLinux, TargetDesktop)
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	versions, err := NewClient(server.Client()).ListVersions(context.Background(), repository)
	if err != nil {
		t.Fatalf("ListVersions() error = %v", err)
	}
	assertVersionStrings(t, versions, []string{"6.8.1"})
}

func TestClientListVersionsReportsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	repository, err := NewRepository(server.URL, HostLinux, TargetDesktop)
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	_, err = NewClient(server.Client()).ListVersions(context.Background(), repository)
	if err == nil || !strings.Contains(err.Error(), "503 Service Unavailable") {
		t.Fatalf("ListVersions() error = %v, want HTTP status", err)
	}
}

func assertVersionStrings(t *testing.T, versions []Version, want []string) {
	t.Helper()
	if len(versions) != len(want) {
		t.Fatalf("version count = %d, want %d (%v)", len(versions), len(want), versions)
	}
	for i, version := range versions {
		if got := version.String(); got != want[i] {
			t.Errorf("version[%d] = %q, want %q", i, got, want[i])
		}
	}
}
