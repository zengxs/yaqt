package qtrepo

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientListArchitecturesDiscoversWindowsVariantRepositories(t *testing.T) {
	version := Version{Major: 6, Minor: 11, Patch: 2}
	variants := map[string]struct {
		architecture     string
		installDirectory string
	}{
		"qt6_6112_msvc2022_64": {
			architecture:     "win64_msvc2022_64",
			installDirectory: "msvc2022_64",
		},
		"qt6_6112_mingw": {
			architecture:     "win64_mingw",
			installDirectory: "mingw_64",
		},
		"qt6_6112_llvm_mingw": {
			architecture:     "win64_llvm_mingw",
			installDirectory: "llvm-mingw_64",
		},
		"qt6_6112_msvc2022_arm64_cross_compiled": {
			architecture:     "win64_msvc2022_arm64_cross_compiled",
			installDirectory: "msvc2022_arm64",
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		const outerPath = "/online/qtsdkrepository/windows_x86/desktop/qt6_6112/"
		if request.URL.Path == outerPath {
			writer.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprint(writer, `<a href="../">parent</a>
<a href="https://attacker.example/qt6_6112_fake/">external</a>
<a href="qt6_6112_msvc2022_arm64_cross_compiled/">ARM64 cross-compiled</a>
<a href="qt6_6112_msvc2022_64/">MSVC</a>
<a href="qt6_6112_mingw/">MinGW</a>
<a href="qt6_6112_llvm_mingw/">LLVM-MinGW</a>
<a href="qt6_6112_src_doc_examples/">sources, documentation, and examples</a>
<a href="unrelated/">unrelated</a>`)
			return
		}

		prefix := strings.TrimSuffix(outerPath, "/") + "/"
		name := strings.TrimSuffix(strings.TrimPrefix(request.URL.Path, prefix), "/Updates.xml")
		variant, ok := variants[name]
		if !ok || request.URL.Path != prefix+name+"/Updates.xml" {
			http.NotFound(writer, request)
			return
		}
		writeArchitectureMetadata(
			writer,
			version,
			variant.architecture,
			variant.installDirectory,
		)
	}))
	defer server.Close()

	repository, err := NewRepository(server.URL, HostWindows, TargetDesktop)
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	architectures, err := NewClient(server.Client()).ListArchitectures(
		context.Background(),
		ArchitectureRequest{Repository: repository, Version: version},
	)
	if err != nil {
		t.Fatalf("ListArchitectures() error = %v", err)
	}
	want := []string{
		"win64_llvm_mingw",
		"win64_mingw",
		"win64_msvc2022_64",
		"win64_msvc2022_arm64_cross_compiled",
	}
	if !equalStrings(architectures, want) {
		t.Errorf("ListArchitectures() = %v, want %v", architectures, want)
	}
}

func TestClientListArchitecturesDiscoversWASMVariants(t *testing.T) {
	version := Version{Major: 6, Minor: 11, Patch: 2}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/online/qtsdkrepository/all_os/wasm/qt6_6112/":
			writer.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprint(writer, `<a href="qt6_6112_wasm_singlethread/">single</a>
<a href="qt6_6112_wasm_multithread/">multi</a>`)
		case "/online/qtsdkrepository/all_os/wasm/qt6_6112/qt6_6112_wasm_singlethread/Updates.xml":
			writeArchitectureMetadata(writer, version, "wasm_singlethread", "wasm_singlethread")
		case "/online/qtsdkrepository/all_os/wasm/qt6_6112/qt6_6112_wasm_multithread/Updates.xml":
			writeArchitectureMetadata(writer, version, "wasm_multithread", "wasm_multithread")
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	repository, err := NewRepository(server.URL, HostMac, TargetWASM)
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	architectures, err := NewClient(server.Client()).ListArchitectures(
		context.Background(),
		ArchitectureRequest{Repository: repository, Version: version},
	)
	if err != nil {
		t.Fatalf("ListArchitectures() error = %v", err)
	}
	want := []string{"wasm_multithread", "wasm_singlethread"}
	if !equalStrings(architectures, want) {
		t.Errorf("ListArchitectures() = %v, want %v", architectures, want)
	}
}

func TestClientListArchitecturesRejectsInvalidVariantMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/online/qtsdkrepository/mac_x64/desktop/qt6_6112/":
			_, _ = fmt.Fprint(writer, `<a href="qt6_6112/">macOS</a>`)
		case "/online/qtsdkrepository/mac_x64/desktop/qt6_6112/qt6_6112/Updates.xml":
			writer.Header().Set("Content-Type", "application/xml")
			_, _ = fmt.Fprint(writer, `<Updates><PackageUpdate><Name>qt.qt6.6112.addons</Name></PackageUpdate></Updates>`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	repository, err := NewRepository(server.URL, HostMac, TargetDesktop)
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	_, err = NewClient(server.Client()).ListArchitectures(
		context.Background(),
		ArchitectureRequest{
			Repository: repository,
			Version:    Version{Major: 6, Minor: 11, Patch: 2},
		},
	)
	if err == nil || !strings.Contains(err.Error(), "contains no usable Qt package architectures") {
		t.Fatalf("ListArchitectures() error = %v, want invalid variant metadata context", err)
	}
}

func TestClientListArchitecturesRejectsUnsupportedTarget(t *testing.T) {
	repository, err := NewRepository(DefaultBaseURL, HostLinux, TargetQt)
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	_, err = NewClient(nil).ListArchitectures(
		context.Background(),
		ArchitectureRequest{
			Repository: repository,
			Version:    Version{Major: 6, Minor: 11, Patch: 2},
		},
	)
	if err == nil || !strings.Contains(err.Error(), `architecture listing does not support target "qt"`) {
		t.Fatalf("ListArchitectures() error = %v, want an unsupported target error", err)
	}
}

func TestClientListArchitecturesRejectsUnsupportedVersionBeforeNetwork(t *testing.T) {
	repository, err := NewRepository(DefaultBaseURL, HostLinux, TargetDesktop)
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	_, err = NewClient(nil).ListArchitectures(
		context.Background(),
		ArchitectureRequest{
			Repository: repository,
			Version:    Version{Major: 6, Minor: 7, Patch: 3},
		},
	)
	if err == nil || !strings.Contains(err.Error(), "minimum supported version is 6.8.0") {
		t.Fatalf("ListArchitectures() error = %v, want the minimum version error", err)
	}
}

func writeArchitectureMetadata(
	writer http.ResponseWriter,
	version Version,
	architecture,
	installDirectory string,
) {
	archive := "qtbase-test.7z"
	writer.Header().Set("Content-Type", "application/xml")
	_, _ = fmt.Fprintf(writer, `<Updates>
 <PackageUpdate>
  <Name>qt.qt%d.%s.%s</Name>
  <DisplayName>%s</DisplayName>
  <Version>%s-0-test</Version>
  <DownloadableArchives>%s</DownloadableArchives>
  <Operations>
   <Operation name="Extract">
    <Argument>@TargetDir@/%s/%s</Argument>
    <Argument>%s</Argument>
   </Operation>
  </Operations>
 </PackageUpdate>
</Updates>`,
		version.Major,
		version.compact(),
		architecture,
		architecture,
		version,
		archive,
		version,
		installDirectory,
		archive,
	)
}
