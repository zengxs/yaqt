package qtinstall

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/zengxs/yaqt/internal/qtrepo"
)

func TestNewMobileRelocatorRequiresHostTools(t *testing.T) {
	qtRoot := filepath.Join(newTestCacheDir(t), "Qt")
	hostQtDir := filepath.Join(qtRoot, "6.8.0", "host")
	writeHostQtTool(t, filepath.Join(hostQtDir, "bin", "qmake6"), qtrepo.HostMac)
	writeRelocationFile(t, filepath.Join(hostQtDir, "mkspecs", "qconfig.pri"), "QT_VERSION = 6.8.0\n", 0o644)

	_, err := NewMobileRelocator(qtrepo.TargetAndroid, testQtInstallationIdentity(qtrepo.HostMac), qtRoot)
	if err == nil || !strings.Contains(err.Error(), "qtpaths6") {
		t.Fatalf("NewMobileRelocator() error = %v, want a missing qtpaths6 error", err)
	}
}

func TestNewMobileRelocatorRequiresMatchingQtVersion(t *testing.T) {
	qtRoot := filepath.Join(newTestCacheDir(t), "Qt")
	hostQtDir := filepath.Join(qtRoot, "6.12.0", "host")
	for _, tool := range []string{"qmake6", "qtpaths6"} {
		writeHostQtTool(t, filepath.Join(hostQtDir, "bin", tool), qtrepo.HostMac)
	}
	writeRelocationFile(t, filepath.Join(hostQtDir, "mkspecs", "qconfig.pri"), "QT_VERSION = 6.8.0\n", 0o644)

	_, err := NewMobileRelocator(qtrepo.TargetAndroid, qtrepo.QtInstallationIdentity{
		Host:    qtrepo.HostMac,
		Version: qtrepo.Version{Major: 6, Minor: 12},
	}, qtRoot)
	if err == nil || !strings.Contains(err.Error(), "contains Qt 6.8.0") {
		t.Fatalf("NewMobileRelocator() error = %v, want a mismatched Qt version error", err)
	}
}

func TestNewMobileRelocatorRejectsAmbiguousHostQtDirectories(t *testing.T) {
	qtRoot, hostQtDir, _ := createAndroidRelocationTree(t, qtrepo.HostMac)
	secondHostQtDir := filepath.Join(filepath.Dir(hostQtDir), "second-host")
	for _, tool := range []string{"qmake6", "qtpaths6"} {
		writeHostQtTool(t, filepath.Join(secondHostQtDir, "bin", tool), qtrepo.HostMac)
	}
	writeRelocationFile(t, filepath.Join(secondHostQtDir, "mkspecs", "qconfig.pri"), "QT_VERSION = 6.8.0\n", 0o644)

	_, err := NewMobileRelocator(qtrepo.TargetAndroid, testQtInstallationIdentity(qtrepo.HostMac), qtRoot)
	if err == nil || !strings.Contains(err.Error(), "multiple desktop Qt") {
		t.Fatalf("NewMobileRelocator() error = %v, want an ambiguous host Qt error", err)
	}
	for _, path := range []string{hostQtDir, secondHostQtDir} {
		if !strings.Contains(err.Error(), path) {
			t.Errorf("NewMobileRelocator() error does not list %q: %v", path, err)
		}
	}
}

func TestNewMobileRelocatorRejectsExecutableForDifferentHost(t *testing.T) {
	qtRoot := filepath.Join(newTestCacheDir(t), "Qt")
	hostQtDir := filepath.Join(qtRoot, "6.8.0", "wrong-host")
	for _, tool := range []string{"qmake6", "qtpaths6"} {
		writeHostQtTool(t, filepath.Join(hostQtDir, "bin", tool), qtrepo.HostLinux)
	}
	writeRelocationFile(
		t,
		filepath.Join(hostQtDir, "mkspecs", "qconfig.pri"),
		"QT_VERSION = 6.8.0\n",
		0o644,
	)

	_, err := NewMobileRelocator(qtrepo.TargetAndroid, testQtInstallationIdentity(qtrepo.HostMac), qtRoot)
	if err == nil || !strings.Contains(err.Error(), "does not match host mac") {
		t.Fatalf("NewMobileRelocator() error = %v, want a host executable mismatch error", err)
	}
}
