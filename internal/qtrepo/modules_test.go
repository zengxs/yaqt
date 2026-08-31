package qtrepo

import (
	"context"
	"testing"
)

func TestClientListDesktopModules(t *testing.T) {
	server := newMetadataServer(
		t,
		"/mirror/online/qtsdkrepository/mac_x64/desktop/qt6_6112/qt6_6112/Updates.xml",
		"testdata/desktop-6.11.2-mac-updates.xml",
	)
	defer server.Close()

	repository, err := NewRepository(server.URL+"/mirror", HostMac, TargetDesktop)
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	modules, err := NewClient(server.Client()).ListModules(context.Background(), ModuleRequest{
		Repository: repository,
		Version:    Version{Major: 6, Minor: 11, Patch: 2},
	})
	if err != nil {
		t.Fatalf("ListModules() error = %v", err)
	}
	if got, want := modules, []string{"qtmultimedia"}; !equalStrings(got, want) {
		t.Errorf("ListModules() = %v, want %v", got, want)
	}
}

func TestClientListAndroidModules(t *testing.T) {
	server := newMetadataServer(
		t,
		"/online/qtsdkrepository/all_os/android/qt6_680/qt6_680_arm64_v8a/Updates.xml",
		"testdata/android-6.8.0-arm64-v8a-updates.xml",
	)
	defer server.Close()

	repository, err := NewRepository(server.URL, HostLinux, TargetAndroid)
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	modules, err := NewClient(server.Client()).ListModules(context.Background(), ModuleRequest{
		Repository: repository,
		Version:    Version{Major: 6, Minor: 8},
		AndroidABI: AndroidABIArm64V8A,
	})
	if err != nil {
		t.Fatalf("ListModules() error = %v", err)
	}
	if got, want := modules, []string{"qtmultimedia"}; !equalStrings(got, want) {
		t.Errorf("ListModules() = %v, want %v", got, want)
	}
}

func TestAvailableModuleNamesFiltersAndSortsInstallablePackages(t *testing.T) {
	version := Version{Major: 6, Minor: 11, Patch: 2}
	metadata := newPackageVariantMetadata(
		"https://mirror.example/Updates.xml",
		version,
		"clang_64",
		"desktop architecture clang_64",
	)
	packages := map[string]packageUpdate{
		"qt.qt6.6112.clang_64": testInstallablePackageUpdate(
			"qt.qt6.6112.clang_64",
			"qtbase.7z",
		),
		"qt.qt6.6112.addons.qtmultimedia.clang_64": testInstallablePackageUpdate(
			"qt.qt6.6112.addons.qtmultimedia.clang_64",
			"qtmultimedia.7z",
		),
		"qt.qt6.6112.qtcharts.clang_64": testInstallablePackageUpdate(
			"qt.qt6.6112.qtcharts.clang_64",
			"qtcharts.7z",
		),
		"qt.qt6.6112.addons.qtcharts.clang_64": testInstallablePackageUpdate(
			"qt.qt6.6112.addons.qtcharts.clang_64",
			"qtcharts-duplicate.7z",
		),
		"qt.qt6.6112.addons.qtspeech.clang_64": {
			Name: "qt.qt6.6112.addons.qtspeech.clang_64",
		},
		"qt.qt6.6112.addons.qtbad.clang_64": {
			Name:                 "qt.qt6.6112.addons.qtbad.clang_64",
			Version:              "6.11.2-0-test",
			DownloadableArchives: "unsafe.zip",
			Operations: []updateOperation{
				{Name: "Extract", Arguments: []string{"@TargetDir@", "unsafe.zip"}},
			},
		},
		"qt.qt6.6112.addons.qtbroken.clang_64": {
			Name:                 "qt.qt6.6112.addons.qtbroken.clang_64",
			Version:              "6.11.2-0-test",
			DownloadableArchives: "qtbroken.7z",
		},
		"qt.qt6.6112.addons.qtmultimedia": testInstallablePackageUpdate(
			"qt.qt6.6112.addons.qtmultimedia",
			"aggregate.7z",
		),
		"qt.qt6.6112.addons.qtwebengine.linux_gcc_64": testInstallablePackageUpdate(
			"qt.qt6.6112.addons.qtwebengine.linux_gcc_64",
			"other-architecture.7z",
		),
		"qt.qt6.6112.extensions.qtfoo.clang_64": testInstallablePackageUpdate(
			"qt.qt6.6112.extensions.qtfoo.clang_64",
			"unsupported-package-shape.7z",
		),
	}

	got := availableModuleNames(packages, metadata, version)
	want := []string{"qtcharts", "qtmultimedia"}
	if !equalStrings(got, want) {
		t.Errorf("availableModuleNames() = %v, want %v", got, want)
	}
}

func TestAvailableModuleNamesUsesInstallCandidatePrecedence(t *testing.T) {
	version := Version{Major: 6, Minor: 11, Patch: 2}
	metadata := newPackageVariantMetadata(
		"https://mirror.example/Updates.xml",
		version,
		"clang_64",
		"desktop architecture clang_64",
	)
	packages := map[string]packageUpdate{
		"qt.qt6.6112.addons.qtcharts.clang_64": {
			Name:    "qt.qt6.6112.addons.qtcharts.clang_64",
			Version: "6.11.2-0-test",
		},
		"qt.qt6.6112.qtcharts.clang_64": testInstallablePackageUpdate(
			"qt.qt6.6112.qtcharts.clang_64",
			"qtcharts.7z",
		),
	}

	got := availableModuleNames(packages, metadata, version)
	if len(got) != 0 {
		t.Errorf("availableModuleNames() = %v, want no modules shadowed by an unusable preferred package", got)
	}
}

func testInstallablePackageUpdate(name, archive string) packageUpdate {
	return packageUpdate{
		Name:                 name,
		Version:              "6.11.2-0-test",
		DownloadableArchives: archive,
		Operations: []updateOperation{
			{Name: "Extract", Arguments: []string{"@TargetDir@", archive}},
		},
	}
}

func TestClientListModulesRejectsUnsupportedTarget(t *testing.T) {
	repository, err := NewRepository(DefaultBaseURL, HostLinux, TargetWASM)
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	_, err = NewClient(nil).ListModules(context.Background(), ModuleRequest{
		Repository: repository,
		Version:    Version{Major: 6, Minor: 8},
	})
	if err == nil {
		t.Fatal("ListModules() error = nil, want an unsupported target error")
	}
}
