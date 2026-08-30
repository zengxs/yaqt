package qtrepo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientResolveAndroidInstallPlanForQt680(t *testing.T) {
	server := newMetadataServer(
		t,
		"/mirror/online/qtsdkrepository/all_os/android/qt6_680/qt6_680_arm64_v8a/Updates.xml",
		"testdata/android-6.8.0-arm64-v8a-updates.xml",
	)
	defer server.Close()

	repository, err := NewRepository(server.URL+"/mirror", HostLinux, TargetAndroid)
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	plan, err := NewClient(server.Client()).ResolveInstall(context.Background(), InstallRequest{
		Repository:  repository,
		Version:     Version{Major: 6, Minor: 8},
		AndroidABIs: []AndroidABI{AndroidABIArm64V8A},
		Modules:     []string{"qtmultimedia"},
		Destination: "/opt/Qt",
	})
	if err != nil {
		t.Fatalf("ResolveInstall() error = %v", err)
	}

	if got, want := plan.Target, TargetAndroid; got != want {
		t.Errorf("plan.Target = %q, want %q", got, want)
	}
	if got, want := plan.HostQt.Host, HostLinux; got != want {
		t.Errorf("plan.HostQt.Host = %q, want %q", got, want)
	}
	if got, want := plan.HostQt.Version.String(), "6.8.0"; got != want {
		t.Errorf("plan.HostQt.Version = %q, want %q", got, want)
	}
	if got, want := len(plan.AndroidKits), 1; got != want {
		t.Fatalf("len(plan.AndroidKits) = %d, want %d", got, want)
	}

	kit := plan.AndroidKits[0]
	if got, want := kit.ABI, AndroidABIArm64V8A; got != want {
		t.Errorf("kit.ABI = %q, want %q", got, want)
	}
	if got, want := kit.Destination, filepath.Join("/opt/Qt", "6.8.0", "android_arm64_v8a"); got != want {
		t.Errorf("kit.Destination = %q, want %q", got, want)
	}
	if got, want := len(kit.Packages), 2; got != want {
		t.Fatalf("len(kit.Packages) = %d, want %d", got, want)
	}
	basePackage := kit.Packages[0]
	if got, want := basePackage.Name, "qt.qt6.680.android_arm64_v8a"; got != want {
		t.Errorf("base package name = %q, want %q", got, want)
	}
	if basePackage.Module != "" {
		t.Errorf("base package module = %q, want empty", basePackage.Module)
	}
	modulePackage := kit.Packages[1]
	if got, want := modulePackage.Name, "qt.qt6.680.addons.qtmultimedia.android_arm64_v8a"; got != want {
		t.Errorf("module package name = %q, want %q", got, want)
	}
	if got, want := modulePackage.Module, "qtmultimedia"; got != want {
		t.Errorf("module package module = %q, want %q", got, want)
	}
	if got, want := archiveNames(flattenArchives(kit.Packages)), []string{"qtbase", "qtsvg", "qtdeclarative", "qtmultimedia"}; !equalStrings(got, want) {
		t.Fatalf("archive names = %v, want %v", got, want)
	}

	first := basePackage.Archives[0]
	wantURL := server.URL + "/mirror/online/qtsdkrepository/all_os/android/qt6_680/qt6_680_arm64_v8a/" +
		"qt.qt6.680.android_arm64_v8a/6.8.0-0-202410030750qtbase-MacOS-Android-ARM64.7z"
	if got := first.URL; got != wantURL {
		t.Errorf("archive URL = %q, want %q", got, wantURL)
	}
	if got, want := first.ChecksumURL, wantURL+".sha1"; got != want {
		t.Errorf("checksum URL = %q, want %q", got, want)
	}
	if got, want := first.ExtractTo, filepath.Clean("/opt/Qt"); got != want {
		t.Errorf("ExtractTo = %q, want %q", got, want)
	}
}

func TestClientResolveAndroidInstallPlanUsesExtractOperations(t *testing.T) {
	server := newMetadataServer(
		t,
		"/mirror/online/qtsdkrepository/all_os/android/qt6_6120/qt6_6120_x86_64/Updates.xml",
		"testdata/android-6.12.0-x86_64-updates.xml",
	)
	defer server.Close()

	repository, err := NewRepository(server.URL+"/mirror", HostMac, TargetAndroid)
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	plan, err := NewClient(server.Client()).ResolveInstall(context.Background(), InstallRequest{
		Repository:  repository,
		Version:     Version{Major: 6, Minor: 12},
		AndroidABIs: []AndroidABI{AndroidABIX8664},
		Destination: "/work/Qt",
	})
	if err != nil {
		t.Fatalf("ResolveInstall() error = %v", err)
	}

	kit := plan.AndroidKits[0]
	wantExtractTo := filepath.Join("/work/Qt", "6.12.0", "android_x86_64")
	for _, archive := range flattenArchives(kit.Packages) {
		if archive.ExtractTo != wantExtractTo {
			t.Errorf("archive %q ExtractTo = %q, want %q", archive.Name, archive.ExtractTo, wantExtractTo)
		}
	}
}

func TestClientResolveAndroidInstallPlanRejectsMissingExtractOperationsAfterQt680(t *testing.T) {
	server := newMetadataServer(
		t,
		"/online/qtsdkrepository/all_os/android/qt6_681/qt6_681_x86_64/Updates.xml",
		"testdata/android-6.8.1-missing-operations-updates.xml",
	)
	defer server.Close()

	repository, err := NewRepository(server.URL, HostLinux, TargetAndroid)
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	_, err = NewClient(server.Client()).ResolveInstall(context.Background(), InstallRequest{
		Repository:  repository,
		Version:     Version{Major: 6, Minor: 8, Patch: 1},
		AndroidABIs: []AndroidABI{AndroidABIX8664},
		Destination: "/opt/Qt",
	})
	if err == nil || !strings.Contains(err.Error(), "contains no Extract operations") {
		t.Fatalf("ResolveInstall() error = %v, want a missing Extract operations error", err)
	}
}

func TestClientResolveAndroidInstallPlanRejectsEscapingExtractPath(t *testing.T) {
	server := newMetadataServer(
		t,
		"/online/qtsdkrepository/all_os/android/qt6_6120/qt6_6120_x86_64/Updates.xml",
		"testdata/android-6.12.0-unsafe-updates.xml",
	)
	defer server.Close()

	repository, err := NewRepository(server.URL, HostLinux, TargetAndroid)
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	_, err = NewClient(server.Client()).ResolveInstall(context.Background(), InstallRequest{
		Repository:  repository,
		Version:     Version{Major: 6, Minor: 12},
		AndroidABIs: []AndroidABI{AndroidABIX8664},
		Destination: "/opt/Qt",
	})
	if err == nil || !strings.Contains(err.Error(), "escapes @TargetDir@") {
		t.Fatalf("ResolveInstall() error = %v, want an escaping path error", err)
	}
}

func TestClientResolveAndroidInstallPlanReportsMissingModule(t *testing.T) {
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
	_, err = NewClient(server.Client()).ResolveInstall(context.Background(), InstallRequest{
		Repository:  repository,
		Version:     Version{Major: 6, Minor: 8},
		AndroidABIs: []AndroidABI{AndroidABIArm64V8A},
		Modules:     []string{"qtcharts"},
		Destination: "/opt/Qt",
	})
	if err == nil || !strings.Contains(err.Error(), "qtcharts") {
		t.Fatalf("ResolveInstall() error = %v, want a missing qtcharts error", err)
	}
}

func TestClientResolveAndroidInstallPlanRejectsUnsupportedVersion(t *testing.T) {
	repository, err := NewRepository(DefaultBaseURL, HostLinux, TargetAndroid)
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	_, err = NewClient(nil).ResolveInstall(context.Background(), InstallRequest{
		Repository:  repository,
		Version:     Version{Major: 6, Minor: 7, Patch: 3},
		AndroidABIs: []AndroidABI{AndroidABIArm64V8A},
		Destination: "/opt/Qt",
	})
	if err == nil || !strings.Contains(err.Error(), "minimum supported version is 6.8.0") {
		t.Fatalf("ResolveInstall() error = %v, want an unsupported version error", err)
	}
}

func newMetadataServer(t *testing.T, wantPath, fixture string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != wantPath {
			t.Errorf("request path = %q, want %q", request.URL.Path, wantPath)
			http.NotFound(writer, request)
			return
		}
		if got, want := request.Header.Get("Accept"), "application/xml, text/xml"; got != want {
			t.Errorf("Accept = %q, want %q", got, want)
		}
		if got, want := request.Header.Get("User-Agent"), userAgent; got != want {
			t.Errorf("User-Agent = %q, want %q", got, want)
		}
		metadata, err := os.ReadFile(fixture)
		if err != nil {
			t.Errorf("read fixture: %v", err)
			http.Error(writer, "fixture unavailable", http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", "application/xml")
		_, _ = writer.Write(metadata)
	}))
}

func archiveNames(archives []Archive) []string {
	names := make([]string, len(archives))
	for index, archive := range archives {
		names[index] = archive.Name
	}
	return names
}

func flattenArchives(packages []PackageSelection) []Archive {
	var archives []Archive
	for _, packageSelection := range packages {
		archives = append(archives, packageSelection.Archives...)
	}
	return archives
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
