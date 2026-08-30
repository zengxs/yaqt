package qtrepo

import "testing"

func TestHostForPlatform(t *testing.T) {
	tests := []struct {
		name   string
		goos   string
		goarch string
		want   Host
	}{
		{name: "Windows x64", goos: "windows", goarch: "amd64", want: HostWindows},
		{name: "Windows ARM64", goos: "windows", goarch: "arm64", want: HostWindowsARM64},
		{name: "macOS", goos: "darwin", goarch: "arm64", want: HostMac},
		{name: "Linux x64", goos: "linux", goarch: "amd64", want: HostLinux},
		{name: "Linux ARM64", goos: "linux", goarch: "arm64", want: HostLinuxARM64},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := HostForPlatform(test.goos, test.goarch)
			if err != nil {
				t.Fatalf("HostForPlatform() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("HostForPlatform() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestHostForPlatformRejectsUnsupportedPlatform(t *testing.T) {
	tests := []struct {
		name   string
		goos   string
		goarch string
	}{
		{name: "unsupported OS", goos: "freebsd", goarch: "amd64"},
		{name: "unsupported Windows architecture", goos: "windows", goarch: "386"},
		{name: "unsupported macOS architecture", goos: "darwin", goarch: "riscv64"},
		{name: "unsupported Linux architecture", goos: "linux", goarch: "riscv64"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := HostForPlatform(test.goos, test.goarch); err == nil {
				t.Fatal("HostForPlatform() error = nil, want an unsupported platform error")
			}
		})
	}
}

func TestNewRepositoryBuildsIndexURL(t *testing.T) {
	repository, err := NewRepository("https://mirror.example/qt/", HostLinux, TargetDesktop)
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}

	want := "https://mirror.example/qt/online/qtsdkrepository/linux_x64/desktop/"
	if got := repository.IndexURL(); got != want {
		t.Fatalf("IndexURL() = %q, want %q", got, want)
	}
}

func TestNewRepositoryRoutesPlatformIndependentTargets(t *testing.T) {
	tests := []struct {
		name   string
		host   Host
		target Target
		want   string
	}{
		{
			name:   "Android",
			host:   HostLinux,
			target: TargetAndroid,
			want:   "https://mirror.example/qt/online/qtsdkrepository/all_os/android/",
		},
		{
			name:   "WebAssembly",
			host:   HostMac,
			target: TargetWASM,
			want:   "https://mirror.example/qt/online/qtsdkrepository/all_os/wasm/",
		},
		{
			name:   "shared Qt content",
			host:   HostWindows,
			target: TargetQt,
			want:   "https://mirror.example/qt/online/qtsdkrepository/all_os/qt/",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository, err := NewRepository("https://mirror.example/qt", test.host, test.target)
			if err != nil {
				t.Fatalf("NewRepository() error = %v", err)
			}
			if got := repository.IndexURL(); got != test.want {
				t.Fatalf("IndexURL() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestNewRepositoryRejectsInvalidHostTargetPair(t *testing.T) {
	if _, err := NewRepository(DefaultBaseURL, HostLinux, TargetIOS); err == nil {
		t.Fatal("NewRepository() error = nil, want an invalid host/target error")
	}
}

func TestParseHostRejectsRepositorySegment(t *testing.T) {
	if _, err := ParseHost("all_os"); err == nil {
		t.Fatal("ParseHost() error = nil, want a repository segment error")
	}
}

func TestNewRepositoryRejectsInvalidBaseURL(t *testing.T) {
	if _, err := NewRepository("file:///srv/qt", HostLinux, TargetDesktop); err == nil {
		t.Fatal("NewRepository() error = nil, want an invalid base URL error")
	}
}
