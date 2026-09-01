package qtrepo

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/zengxs/yaqt/internal/buildinfo"
)

func TestClientListDesktopModulesIncludesExtensionRepositories(t *testing.T) {
	fixtures := map[string]string{
		"/online/qtsdkrepository/mac_x64/desktop/qt6_6112/qt6_6112/Updates.xml":            "testdata/desktop-6.11.2-mac-updates.xml",
		"/online/qtsdkrepository/mac_x64/extensions/qtwebengine/6112/clang_64/Updates.xml": "testdata/extension-qtwebengine-6.11.2-mac-updates.xml",
		"/online/qtsdkrepository/mac_x64/extensions/qtpdf/6112/clang_64/Updates.xml":       "testdata/extension-qtpdf-6.11.2-mac-updates.xml",
	}
	server := newModuleMetadataServer(t, fixtures)
	defer server.Close()

	repository, err := NewRepository(server.URL, HostMac, TargetDesktop)
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
	if got, want := modules, []string{"qtmultimedia", "qtpdf", "qtwebengine"}; !equalStrings(got, want) {
		t.Errorf("ListModules() = %v, want %v", got, want)
	}
}

func TestClientListModulesDeduplicatesMainAndExtensionModules(t *testing.T) {
	fixtures := map[string]string{
		"/online/qtsdkrepository/mac_x64/desktop/qt6_6112/qt6_6112/Updates.xml":      "testdata/desktop-6.11.2-mac-qtpdf-updates.xml",
		"/online/qtsdkrepository/mac_x64/extensions/qtpdf/6112/clang_64/Updates.xml": "testdata/extension-qtpdf-6.11.2-mac-updates.xml",
	}
	server := newModuleMetadataServer(t, fixtures)
	defer server.Close()

	repository, err := NewRepository(server.URL, HostMac, TargetDesktop)
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
	if got, want := modules, []string{"qtpdf"}; !equalStrings(got, want) {
		t.Errorf("ListModules() = %v, want %v", got, want)
	}
}

func TestClientListModulesReportsInvalidExtensionMetadata(t *testing.T) {
	server := newModuleMetadataServer(t, map[string]string{
		"/online/qtsdkrepository/mac_x64/desktop/qt6_6112/qt6_6112/Updates.xml":      "testdata/desktop-6.11.2-mac-updates.xml",
		"/online/qtsdkrepository/mac_x64/extensions/qtpdf/6112/clang_64/Updates.xml": "testdata/extension-qtpdf-6.11.2-invalid-updates.xml",
	})
	defer server.Close()

	repository, err := NewRepository(server.URL, HostMac, TargetDesktop)
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	_, err = NewClient(server.Client()).ListModules(context.Background(), ModuleRequest{
		Repository: repository,
		Version:    Version{Major: 6, Minor: 11, Patch: 2},
	})
	if err == nil || !strings.Contains(err.Error(), `validate extension module "qtpdf"`) {
		t.Fatalf("ListModules() error = %v, want qtpdf validation context", err)
	}
	if !strings.Contains(err.Error(), "contains no Extract operations") {
		t.Errorf("ListModules() error = %v, want the metadata validation failure", err)
	}
}

func TestClientListAndroidModulesIncludesAvailableExtensions(t *testing.T) {
	fixtures := map[string]string{
		"/online/qtsdkrepository/all_os/android/qt6_680/qt6_680_arm64_v8a/Updates.xml":      "testdata/android-6.8.0-arm64-v8a-updates.xml",
		"/online/qtsdkrepository/all_os/extensions/qtpdf/680/qt6_680_arm64_v8a/Updates.xml": "testdata/extension-qtpdf-6.8.0-android-arm64-v8a-updates.xml",
	}
	server := newModuleMetadataServer(t, fixtures)
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
	if got, want := modules, []string{"qtmultimedia", "qtpdf"}; !equalStrings(got, want) {
		t.Errorf("ListModules() = %v, want %v", got, want)
	}
}

func TestClientListDesktopModulesUsesExtensionRepositoryArchitecture(t *testing.T) {
	tests := []struct {
		name                  string
		host                  Host
		mainMetadataPath      string
		extensionMetadataPath string
		basePackage           string
		extensionPackage      string
	}{
		{
			name:                  "Linux x64",
			host:                  HostLinux,
			mainMetadataPath:      "/online/qtsdkrepository/linux_x64/desktop/qt6_6100/qt6_6100/Updates.xml",
			extensionMetadataPath: "/online/qtsdkrepository/linux_x64/extensions/qtpdf/6100/x86_64/Updates.xml",
			basePackage:           "qt.qt6.6100.linux_gcc_64",
			extensionPackage:      "extensions.qtpdf.6100.linux_gcc_64",
		},
		{
			name:                  "Linux ARM64",
			host:                  HostLinuxARM64,
			mainMetadataPath:      "/online/qtsdkrepository/linux_arm64/desktop/qt6_6100/qt6_6100/Updates.xml",
			extensionMetadataPath: "/online/qtsdkrepository/linux_arm64/extensions/qtpdf/6100/arm64/Updates.xml",
			basePackage:           "qt.qt6.6100.linux_gcc_arm64",
			extensionPackage:      "extensions.qtpdf.6100.linux_gcc_arm64",
		},
		{
			name:                  "Windows x64",
			host:                  HostWindows,
			mainMetadataPath:      "/online/qtsdkrepository/windows_x86/desktop/qt6_6100/qt6_6100/Updates.xml",
			extensionMetadataPath: "/online/qtsdkrepository/windows_x86/extensions/qtpdf/6100/msvc2022_64/Updates.xml",
			basePackage:           "qt.qt6.6100.win64_msvc2022_64",
			extensionPackage:      "extensions.qtpdf.6100.win64_msvc2022_64",
		},
		{
			name:                  "Windows ARM64",
			host:                  HostWindowsARM64,
			mainMetadataPath:      "/online/qtsdkrepository/windows_arm64/desktop/qt6_6100/qt6_6100/Updates.xml",
			extensionMetadataPath: "/online/qtsdkrepository/windows_arm64/extensions/qtpdf/6100/msvc2022_arm64/Updates.xml",
			basePackage:           "qt.qt6.6100.win64_msvc2022_arm64",
			extensionPackage:      "extensions.qtpdf.6100.win64_msvc2022_arm64",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case test.mainMetadataPath:
					writeTestPackageMetadata(writer, test.basePackage, "qtbase.7z")
				case test.extensionMetadataPath:
					writeTestPackageMetadata(writer, test.extensionPackage, "qtpdf.7z")
				default:
					http.NotFound(writer, request)
				}
			}))
			defer server.Close()

			repository, err := NewRepository(server.URL, test.host, TargetDesktop)
			if err != nil {
				t.Fatalf("NewRepository() error = %v", err)
			}
			modules, err := NewClient(server.Client()).ListModules(context.Background(), ModuleRequest{
				Repository: repository,
				Version:    Version{Major: 6, Minor: 10},
			})
			if err != nil {
				t.Fatalf("ListModules() error = %v", err)
			}
			if got, want := modules, []string{"qtpdf"}; !equalStrings(got, want) {
				t.Errorf("ListModules() = %v, want %v", got, want)
			}
		})
	}
}

func writeTestPackageMetadata(writer http.ResponseWriter, packageName, archiveName string) {
	writer.Header().Set("Content-Type", "application/xml")
	_, _ = fmt.Fprintf(writer, `<Updates>
 <PackageUpdate>
  <Name>%s</Name>
  <Version>6.10.0-0-test</Version>
  <DownloadableArchives>%s</DownloadableArchives>
  <Operations>
   <Operation name="Extract">
    <Argument>@TargetDir@</Argument>
    <Argument>%s</Argument>
   </Operation>
  </Operations>
 </PackageUpdate>
</Updates>`, packageName, archiveName, archiveName)
}

func newModuleMetadataServer(
	t *testing.T,
	fixtures map[string]string,
) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		fixture, ok := fixtures[request.URL.Path]
		if !ok {
			http.NotFound(writer, request)
			return
		}
		if got, want := request.Header.Get("Accept"), "application/xml, text/xml"; got != want {
			t.Errorf("Accept = %q, want %q", got, want)
		}
		if got, want := request.Header.Get("User-Agent"), buildinfo.UserAgent; got != want {
			t.Errorf("User-Agent = %q, want %q", got, want)
		}
		contents, err := os.ReadFile(fixture)
		if err != nil {
			t.Errorf("read fixture: %v", err)
			http.Error(writer, "fixture unavailable", http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", "application/xml")
		_, _ = writer.Write(contents)
	}))
}

func TestClientListDesktopModules(t *testing.T) {
	server := newModuleMetadataServer(t, map[string]string{
		"/mirror/online/qtsdkrepository/mac_x64/desktop/qt6_6112/qt6_6112/Updates.xml": "testdata/desktop-6.11.2-mac-updates.xml",
	})
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
	server := newModuleMetadataServer(t, map[string]string{
		"/online/qtsdkrepository/all_os/android/qt6_680/qt6_680_arm64_v8a/Updates.xml": "testdata/android-6.8.0-arm64-v8a-updates.xml",
	})
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

func TestClientListIOSModules(t *testing.T) {
	server := newModuleMetadataServer(t, map[string]string{
		"/online/qtsdkrepository/mac_x64/ios/qt6_6112/qt6_6112/Updates.xml": "testdata/ios-6.11.2-updates.xml",
	})
	defer server.Close()

	repository, err := NewRepository(server.URL, HostMac, TargetIOS)
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
