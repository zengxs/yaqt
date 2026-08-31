package qtinstall

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zengxs/yaqt/internal/qtrepo"
)

func TestDesktopRelocatorConfiguresExtractedKit(t *testing.T) {
	kitDir := createDesktopRelocationTree(t, qtrepo.HostMac, "6.11.2")
	writeRelocationFile(
		t,
		filepath.Join(kitDir, "lib", "pkgconfig", "Qt6Core.pc"),
		"prefix=/Users/qt/work/install\nLibs: -F/Users/qt/work/install/lib -framework QtCore\n",
		0o644,
	)
	writeRelocationFile(
		t,
		filepath.Join(kitDir, "lib", "libQt6Example.prl"),
		"QMAKE_PRL_LIBS = /Users/qt/work/install/lib/libExample.a\n",
		0o644,
	)
	writeRelocationFile(
		t,
		filepath.Join(kitDir, "lib", "libQt6Example.la"),
		"libdir='/Users/qt/work/install/lib'\n",
		0o644,
	)

	relocator, err := NewDesktopRelocator(qtrepo.QtInstallationIdentity{
		Host:    qtrepo.HostMac,
		Version: qtrepo.Version{Major: 6, Minor: 11, Patch: 2},
	})
	if err != nil {
		t.Fatalf("NewDesktopRelocator() error = %v", err)
	}
	if err := relocator.Relocate(context.Background(), kitDir); err != nil {
		t.Fatalf("Relocate() error = %v", err)
	}

	if got, want := readRelocationFile(t, filepath.Join(kitDir, "bin", "qt.conf")), desktopQtConfiguration; got != want {
		t.Errorf("qt.conf = %q, want %q", got, want)
	}
	packageConfig := readRelocationFile(t, filepath.Join(kitDir, "lib", "pkgconfig", "Qt6Core.pc"))
	if strings.Contains(packageConfig, "/Users/qt/work/install") || !strings.Contains(packageConfig, "prefix="+kitDir) {
		t.Errorf("package config was not relocated:\n%s", packageConfig)
	}
	prl := readRelocationFile(t, filepath.Join(kitDir, "lib", "libQt6Example.prl"))
	if strings.Contains(prl, "/Users/qt/work/install") || !strings.Contains(prl, "$$[QT_INSTALL_LIBS]") {
		t.Errorf("PRL was not relocated:\n%s", prl)
	}
	libtool := readRelocationFile(t, filepath.Join(kitDir, "lib", "libQt6Example.la"))
	if strings.Contains(libtool, "/Users/qt/work/install") || !strings.Contains(libtool, kitDir) {
		t.Errorf("libtool metadata was not relocated:\n%s", libtool)
	}
}

func TestDesktopRelocatorValidatesKitBeforeWriting(t *testing.T) {
	kitDir := createDesktopRelocationTree(t, qtrepo.HostMac, "6.8.0")
	relocator, err := NewDesktopRelocator(qtrepo.QtInstallationIdentity{
		Host:    qtrepo.HostMac,
		Version: qtrepo.Version{Major: 6, Minor: 11, Patch: 2},
	})
	if err != nil {
		t.Fatalf("NewDesktopRelocator() error = %v", err)
	}
	err = relocator.Relocate(context.Background(), kitDir)
	if err == nil || !strings.Contains(err.Error(), "Qt 6.8.0") {
		t.Fatalf("Relocate() error = %v, want a version mismatch", err)
	}
	if _, statErr := os.Stat(filepath.Join(kitDir, "bin", "qt.conf")); !os.IsNotExist(statErr) {
		t.Fatalf("qt.conf was written before validation; os.Stat() error = %v", statErr)
	}
}

func TestDesktopRelocatorHonorsCanceledContext(t *testing.T) {
	kitDir := createDesktopRelocationTree(t, qtrepo.HostMac, "6.11.2")
	relocator, err := NewDesktopRelocator(qtrepo.QtInstallationIdentity{
		Host:    qtrepo.HostMac,
		Version: qtrepo.Version{Major: 6, Minor: 11, Patch: 2},
	})
	if err != nil {
		t.Fatalf("NewDesktopRelocator() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := relocator.Relocate(ctx, kitDir); err == nil {
		t.Fatal("Relocate() error = nil, want cancellation")
	}
	if _, statErr := os.Stat(filepath.Join(kitDir, "bin", "qt.conf")); !os.IsNotExist(statErr) {
		t.Fatalf("qt.conf was written after cancellation; os.Stat() error = %v", statErr)
	}
}

func createDesktopRelocationTree(t *testing.T, host qtrepo.Host, version string) string {
	t.Helper()
	kitDir := filepath.Join(newTestCacheDir(t), "Qt", version, "desktop")
	toolExtension := ""
	if host == qtrepo.HostWindows || host == qtrepo.HostWindowsARM64 {
		toolExtension = ".exe"
	}
	for _, tool := range []string{"qmake6", "qtpaths6"} {
		writeHostQtTool(t, filepath.Join(kitDir, "bin", tool+toolExtension), host)
	}
	writeRelocationFile(
		t,
		filepath.Join(kitDir, "mkspecs", "qconfig.pri"),
		"QT_VERSION = "+version+"\n",
		0o644,
	)
	return kitDir
}
