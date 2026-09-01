package qtinstall

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zengxs/yaqt/internal/qtrepo"
)

func TestReconcileInstallationRejectsKitDirectorySymlinkEscape(t *testing.T) {
	root := newTestCacheDir(t)
	outside := newTestCacheDir(t)
	versionDirectory := filepath.Join(root, "6.11.2")
	if err := os.MkdirAll(versionDirectory, 0o755); err != nil {
		t.Fatalf("create version directory: %v", err)
	}
	kitDirectory := filepath.Join(versionDirectory, "macos")
	if err := os.Symlink(outside, kitDirectory); err != nil {
		t.Skipf("create kit directory symlink: %v", err)
	}

	_, err := ReconcileInstallation(
		InstallationIdentity{
			Version: qtrepo.Version{Major: 6, Minor: 11, Patch: 2},
			Host:    qtrepo.HostMac,
			Target:  qtrepo.TargetDesktop,
		},
		root,
		[]KitRequest{{
			Architecture: string(qtrepo.DesktopArchitectureMacClang64),
			Destination:  kitDirectory,
			Packages: []qtrepo.PackageSelection{{
				Name:           "qt.qt6.6112.clang_64",
				PackageVersion: "6.11.2-0-test",
			}},
		}},
	)
	if err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("ReconcileInstallation() error = %v, want a kit path escape error", err)
	}
}

func TestReconcileInstallationAllowsKitSymlinkWithinRoot(t *testing.T) {
	root := newTestCacheDir(t)
	versionDirectory := filepath.Join(root, "6.11.2")
	realKitDirectory := filepath.Join(versionDirectory, "real-macos")
	if err := os.MkdirAll(realKitDirectory, 0o755); err != nil {
		t.Fatalf("create real kit directory: %v", err)
	}
	kitDirectory := filepath.Join(versionDirectory, "macos")
	if err := os.Symlink("real-macos", kitDirectory); err != nil {
		t.Skipf("create kit directory symlink: %v", err)
	}

	reconciliation, err := ReconcileInstallation(
		InstallationIdentity{
			Version: qtrepo.Version{Major: 6, Minor: 11, Patch: 2},
			Host:    qtrepo.HostMac,
			Target:  qtrepo.TargetDesktop,
		},
		root,
		[]KitRequest{{
			Architecture: string(qtrepo.DesktopArchitectureMacClang64),
			Destination:  kitDirectory,
			Packages: []qtrepo.PackageSelection{{
				Name:           "qt.qt6.6112.clang_64",
				PackageVersion: "6.11.2-0-test",
			}},
		}},
	)
	if err != nil {
		t.Fatalf("ReconcileInstallation() error = %v", err)
	}
	if got, want := reconciliation.Kits[0].Packages[0].Action, PackageActionAdopt; got != want {
		t.Errorf("package action = %q, want %q", got, want)
	}
}

func TestReconcileInstallationAllowsStateDirectorySymlinkWithinRoot(t *testing.T) {
	root := newTestCacheDir(t)
	kitDirectory := filepath.Join(root, "6.11.2", "macos")
	sharedStateDirectory := filepath.Join(root, "shared-state")
	if err := os.MkdirAll(kitDirectory, 0o755); err != nil {
		t.Fatalf("create kit directory: %v", err)
	}
	if err := os.MkdirAll(sharedStateDirectory, 0o755); err != nil {
		t.Fatalf("create shared state directory: %v", err)
	}
	stateTarget, err := filepath.Rel(kitDirectory, sharedStateDirectory)
	if err != nil {
		t.Fatalf("resolve shared state target: %v", err)
	}
	if err := os.Symlink(stateTarget, filepath.Join(kitDirectory, ".yaqt")); err != nil {
		t.Skipf("create state directory symlink: %v", err)
	}

	reconciliation, err := ReconcileInstallation(
		InstallationIdentity{
			Version: qtrepo.Version{Major: 6, Minor: 11, Patch: 2},
			Host:    qtrepo.HostMac,
			Target:  qtrepo.TargetDesktop,
		},
		root,
		[]KitRequest{{
			Architecture: string(qtrepo.DesktopArchitectureMacClang64),
			Destination:  kitDirectory,
			Packages: []qtrepo.PackageSelection{{
				Name:           "qt.qt6.6112.clang_64",
				PackageVersion: "6.11.2-0-test",
			}},
		}},
	)
	if err != nil {
		t.Fatalf("ReconcileInstallation() error = %v", err)
	}
	if got, want := reconciliation.Kits[0].Packages[0].Action, PackageActionAdopt; got != want {
		t.Errorf("package action = %q, want %q", got, want)
	}
}

func TestReconcileInstallationAllowsManifestSymlinkWithinRoot(t *testing.T) {
	root := newTestCacheDir(t)
	kitDirectory := filepath.Join(root, "6.11.2", "macos")
	stateDirectory := filepath.Join(kitDirectory, ".yaqt")
	sharedStateDirectory := filepath.Join(root, "shared-state")
	if err := os.MkdirAll(stateDirectory, 0o755); err != nil {
		t.Fatalf("create kit state directory: %v", err)
	}
	if err := os.MkdirAll(sharedStateDirectory, 0o755); err != nil {
		t.Fatalf("create shared state directory: %v", err)
	}
	manifest := kitManifest{
		SchemaVersion: manifestSchemaVersion,
		QtVersion:     "6.11.2",
		Host:          qtrepo.HostMac,
		Target:        qtrepo.TargetDesktop,
		Architecture:  string(qtrepo.DesktopArchitectureMacClang64),
		Packages: map[string]manifestPackage{
			"qt.qt6.6112.clang_64": {
				Version: "6.11.2-0-test",
			},
		},
	}
	if err := writeKitManifest(root, filepath.Join(root, "shared-kit"), manifest); err != nil {
		t.Fatalf("write shared manifest: %v", err)
	}
	sharedManifestPath := filepath.Join(root, "shared-kit", ".yaqt", "manifest.json")
	manifestTarget, err := filepath.Rel(stateDirectory, sharedManifestPath)
	if err != nil {
		t.Fatalf("resolve shared manifest target: %v", err)
	}
	if err := os.Symlink(manifestTarget, filepath.Join(stateDirectory, "manifest.json")); err != nil {
		t.Skipf("create manifest symlink: %v", err)
	}

	reconciliation, err := ReconcileInstallation(
		InstallationIdentity{
			Version: qtrepo.Version{Major: 6, Minor: 11, Patch: 2},
			Host:    qtrepo.HostMac,
			Target:  qtrepo.TargetDesktop,
		},
		root,
		[]KitRequest{{
			Architecture: string(qtrepo.DesktopArchitectureMacClang64),
			Destination:  kitDirectory,
			Packages: []qtrepo.PackageSelection{{
				Name:           "qt.qt6.6112.clang_64",
				PackageVersion: "6.11.2-0-test",
			}},
		}},
	)
	if err != nil {
		t.Fatalf("ReconcileInstallation() error = %v", err)
	}
	if got, want := reconciliation.Kits[0].Packages[0].Action, PackageActionSkip; got != want {
		t.Errorf("package action = %q, want %q", got, want)
	}
}

func TestReconcileInstallationRejectsManifestSymlinkEscape(t *testing.T) {
	root := newTestCacheDir(t)
	outside := newTestCacheDir(t)
	kitDirectory := filepath.Join(root, "6.11.2", "macos")
	stateDirectory := filepath.Join(kitDirectory, ".yaqt")
	if err := os.MkdirAll(stateDirectory, 0o755); err != nil {
		t.Fatalf("create kit state directory: %v", err)
	}
	outsideManifestPath := filepath.Join(outside, "manifest.json")
	if err := os.WriteFile(outsideManifestPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write outside manifest: %v", err)
	}
	if err := os.Symlink(outsideManifestPath, filepath.Join(stateDirectory, "manifest.json")); err != nil {
		t.Skipf("create manifest symlink: %v", err)
	}

	_, err := ReconcileInstallation(
		InstallationIdentity{
			Version: qtrepo.Version{Major: 6, Minor: 11, Patch: 2},
			Host:    qtrepo.HostMac,
			Target:  qtrepo.TargetDesktop,
		},
		root,
		[]KitRequest{{
			Architecture: string(qtrepo.DesktopArchitectureMacClang64),
			Destination:  kitDirectory,
			Packages: []qtrepo.PackageSelection{{
				Name:           "qt.qt6.6112.clang_64",
				PackageVersion: "6.11.2-0-test",
			}},
		}},
	)
	if err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("ReconcileInstallation() error = %v, want a manifest path escape error", err)
	}
}
