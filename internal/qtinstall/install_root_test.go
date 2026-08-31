package qtinstall

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zengxs/yaqt/internal/qtrepo"
)

func TestResolveInstallRootReturnsAbsoluteRoot(t *testing.T) {
	root := filepath.Join(newTestCacheDir(t), "Qt")
	got, err := ResolveInstallRoot(root, qtrepo.Version{Major: 6, Minor: 11, Patch: 2})
	if err != nil {
		t.Fatalf("ResolveInstallRoot() error = %v", err)
	}
	want, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("filepath.Abs() error = %v", err)
	}
	if got != want {
		t.Fatalf("ResolveInstallRoot() = %q, want %q", got, want)
	}
}

func TestResolveInstallRootAllowsMissingDirectory(t *testing.T) {
	root := filepath.Join(newTestCacheDir(t), "missing", "Qt")
	if _, err := ResolveInstallRoot(root, qtrepo.Version{Major: 6, Minor: 11, Patch: 2}); err != nil {
		t.Fatalf("ResolveInstallRoot() error = %v", err)
	}
}

func TestResolveInstallRootRejectsVersionDirectory(t *testing.T) {
	root := filepath.Join(newTestCacheDir(t), "Qt", "6.11.2")
	_, err := ResolveInstallRoot(root, qtrepo.Version{Major: 6, Minor: 11, Patch: 2})
	if err == nil || !strings.Contains(err.Error(), "includes version 6.11.2") {
		t.Fatalf("ResolveInstallRoot() error = %v, want a version directory error", err)
	}
}

func TestResolveInstallRootRejectsExistingFile(t *testing.T) {
	root := filepath.Join(newTestCacheDir(t), "Qt")
	if err := os.WriteFile(root, []byte("not a directory\n"), 0o644); err != nil {
		t.Fatalf("write root file: %v", err)
	}
	_, err := ResolveInstallRoot(root, qtrepo.Version{Major: 6, Minor: 11, Patch: 2})
	if err == nil || !strings.Contains(err.Error(), "is not a directory") {
		t.Fatalf("ResolveInstallRoot() error = %v, want a non-directory error", err)
	}
}
