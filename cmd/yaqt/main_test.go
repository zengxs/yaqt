package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/urfave/cli/v3"

	yaqtcache "github.com/zengxs/yaqt/internal/cache"
	"github.com/zengxs/yaqt/internal/qtrepo"
)

type stubRepositoryClient struct {
	repository          qtrepo.Repository
	versions            []qtrepo.Version
	architectures       []string
	architectureRequest qtrepo.ArchitectureRequest
	modules             []string
	moduleRequest       qtrepo.ModuleRequest
	installRequest      qtrepo.InstallRequest
	installPlan         qtrepo.InstallPlan
	cacheRoot           string
}

type stubArchiveFetcher struct {
	destination string
	archives    []qtrepo.Archive
	failOn      string
	events      *[]string
}

type archiveExtraction struct {
	archivePath string
	destination string
}

type stubArchiveExtractor struct {
	extractions []archiveExtraction
	events      *[]string
}

type stubInstallRelocator struct {
	kitDirs []string
	events  *[]string
	failOn  string
}

type blockingArchiveFetcher struct {
	destination string
	entered     chan<- struct{}
	release     <-chan struct{}
}

func (*stubInstallRelocator) Validate(context.Context, string) error {
	return nil
}

func (stub *stubArchiveFetcher) Fetch(_ context.Context, archive qtrepo.Archive) (string, error) {
	stub.archives = append(stub.archives, archive)
	if stub.events != nil {
		*stub.events = append(*stub.events, "fetch "+archive.Name)
	}
	if archive.Name == stub.failOn {
		return "", errors.New("download failed")
	}
	return filepath.Join(stub.destination, archive.Name+".7z"), nil
}

func (fetcher *blockingArchiveFetcher) Fetch(
	ctx context.Context,
	archive qtrepo.Archive,
) (string, error) {
	select {
	case fetcher.entered <- struct{}{}:
	default:
	}
	select {
	case <-fetcher.release:
		return filepath.Join(fetcher.destination, archive.Name+".7z"), nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (stub *stubArchiveExtractor) Extract(_ context.Context, archivePath, destination string) error {
	stub.extractions = append(stub.extractions, archiveExtraction{
		archivePath: archivePath,
		destination: destination,
	})
	if stub.events != nil {
		*stub.events = append(*stub.events, "extract "+filepath.Base(archivePath))
	}
	return nil
}

func (stub *stubInstallRelocator) Relocate(_ context.Context, kitDir string) error {
	stub.kitDirs = append(stub.kitDirs, kitDir)
	if stub.events != nil {
		*stub.events = append(*stub.events, "relocate "+filepath.Base(kitDir))
	}
	if filepath.Base(kitDir) == stub.failOn {
		return errors.New("relocation failed")
	}
	return nil
}

func (stub *stubRepositoryClient) ListVersions(ctx context.Context, repository qtrepo.Repository) ([]qtrepo.Version, error) {
	stub.captureCacheRoot(ctx)
	stub.repository = repository
	return stub.versions, nil
}

func (stub *stubRepositoryClient) ListArchitectures(
	ctx context.Context,
	request qtrepo.ArchitectureRequest,
) ([]string, error) {
	stub.captureCacheRoot(ctx)
	stub.architectureRequest = request
	return stub.architectures, nil
}

func (stub *stubRepositoryClient) ListModules(
	ctx context.Context,
	request qtrepo.ModuleRequest,
) ([]string, error) {
	stub.captureCacheRoot(ctx)
	stub.moduleRequest = request
	return stub.modules, nil
}

func (stub *stubRepositoryClient) ResolveInstall(
	ctx context.Context,
	request qtrepo.InstallRequest,
) (qtrepo.InstallPlan, error) {
	stub.captureCacheRoot(ctx)
	stub.installRequest = request
	return stub.installPlan, nil
}

func (stub *stubRepositoryClient) captureCacheRoot(ctx context.Context) {
	stub.cacheRoot, _ = yaqtcache.RootFromContext(ctx)
}

func cleanInstallTestRoot(t *testing.T, name string) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".tmp", "install-tests", name))
	if err != nil {
		t.Fatalf("filepath.Abs() error = %v", err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatalf("remove previous test root: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Errorf("remove test root: %v", err)
		}
		_ = os.Remove(filepath.Dir(root))
	})
	return root
}

func TestListQtCommand(t *testing.T) {
	output := &bytes.Buffer{}
	cacheRoot := filepath.Join(".tmp", "list-qt-cache")
	lister := &stubRepositoryClient{
		versions: []qtrepo.Version{
			{Major: 6, Minor: 8, Patch: 0},
			{Major: 6, Minor: 8, Patch: 1},
		},
	}
	command := newCommand(
		commandDependencies{versionLister: lister},
		qtrepo.HostLinux,
		output,
		&bytes.Buffer{},
	)

	err := command.Run(context.Background(), []string{
		"yaqt",
		"list-qt",
		"--target", "wasm",
		"--base-url", "https://mirror.example/qt",
		"--cache-dir", cacheRoot,
	})
	if err != nil {
		t.Fatalf("command.Run() error = %v", err)
	}

	if got, want := output.String(), "6.8.0\n6.8.1\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
	if got, want := lister.repository.IndexURL(), "https://mirror.example/qt/online/qtsdkrepository/all_os/wasm/"; got != want {
		t.Fatalf("repository URL = %q, want %q", got, want)
	}
	if got, want := lister.cacheRoot, filepath.Clean(cacheRoot); got != want {
		t.Fatalf("cache root = %q, want %q", got, want)
	}
}

func TestListQtCommandRejectsInvalidHostTargetPair(t *testing.T) {
	client := &stubRepositoryClient{}
	command := newCommand(
		commandDependencies{versionLister: client},
		qtrepo.HostLinux,
		&bytes.Buffer{},
		&bytes.Buffer{},
	)
	err := command.Run(context.Background(), []string{
		"yaqt",
		"list-qt",
		"--host", "linux",
		"--target", "ios",
	})
	if err == nil {
		t.Fatal("command.Run() error = nil, want an invalid host/target error")
	}
}

func TestInstallQtDryRun(t *testing.T) {
	output := &bytes.Buffer{}
	client := &stubRepositoryClient{
		installPlan: qtrepo.InstallPlan{
			Version: qtrepo.Version{Major: 6, Minor: 8},
			Host:    qtrepo.HostLinux,
			Target:  qtrepo.TargetAndroid,
			HostQt: &qtrepo.QtInstallationIdentity{
				Host:    qtrepo.HostLinux,
				Version: qtrepo.Version{Major: 6, Minor: 8},
			},
			AndroidKits: []qtrepo.AndroidKit{
				{
					ABI:         qtrepo.AndroidABIArm64V8A,
					Destination: "/opt/Qt/6.8.0/android_arm64_v8a",
					Packages: []qtrepo.PackageSelection{
						{
							Name:           "qt.qt6.680.android_arm64_v8a",
							PackageVersion: "6.8.0-0-test",
							Archives: []qtrepo.Archive{
								{
									Name: "qtbase",
									URL:  "https://mirror.example/qtbase.7z",
									Checksum: qtrepo.Checksum{
										Algorithm: qtrepo.ChecksumSHA256,
										URL:       "https://mirror.example/qtbase.7z.sha256",
									},
									ExtractTo: "/opt/Qt",
								},
							},
						},
						{
							Name:           "qt.qt6.680.addons.qtmultimedia.android_arm64_v8a",
							PackageVersion: "6.8.0-0-test",
							Module:         "qtmultimedia",
							Archives: []qtrepo.Archive{
								{
									Name: "qtmultimedia",
									URL:  "https://mirror.example/qtmultimedia.7z",
									Checksum: qtrepo.Checksum{
										Algorithm: qtrepo.ChecksumSHA256,
										URL:       "https://mirror.example/qtmultimedia.7z.sha256",
									},
									ExtractTo: "/opt/Qt",
								},
							},
						},
					},
				},
			},
		},
	}
	command := newCommand(
		commandDependencies{
			install: installCommandDependencies{resolver: client},
		},
		qtrepo.HostLinux,
		output,
		&bytes.Buffer{},
	)

	err := command.Run(context.Background(), []string{
		"yaqt",
		"install-qt",
		"6.8.0",
		"--target", "android",
		"--abi", "arm64-v8a",
		"--module", "qtmultimedia",
		"--root", "/opt/Qt",
		"--base-url", "https://mirror.example/qt",
		"--dry-run",
	})
	if err != nil {
		t.Fatalf("command.Run() error = %v", err)
	}

	request := client.installRequest
	if got, want := request.Repository.IndexURL(), "https://mirror.example/qt/online/qtsdkrepository/all_os/android/"; got != want {
		t.Errorf("repository URL = %q, want %q", got, want)
	}
	if got, want := request.Version.String(), "6.8.0"; got != want {
		t.Errorf("version = %q, want %q", got, want)
	}
	if got, want := request.AndroidABIs, []qtrepo.AndroidABI{qtrepo.AndroidABIArm64V8A}; len(got) != len(want) || got[0] != want[0] {
		t.Errorf("AndroidABIs = %v, want %v", got, want)
	}
	if got, want := request.Modules, []string{"qtmultimedia"}; len(got) != len(want) || got[0] != want[0] {
		t.Errorf("Modules = %v, want %v", got, want)
	}
	if got, want := request.Destination, "/opt/Qt"; got != want {
		t.Errorf("Destination = %q, want %q", got, want)
	}

	for _, want := range []string{
		"Qt 6.8.0 for android",
		"Host Qt requirement: linux desktop 6.8.0",
		"arm64-v8a -> /opt/Qt/6.8.0/android_arm64_v8a",
		"base package: qt.qt6.680.android_arm64_v8a",
		"qtbase",
		"https://mirror.example/qtbase.7z.sha256",
		"module qtmultimedia: qt.qt6.680.addons.qtmultimedia.android_arm64_v8a",
		"Post-install: relocate Android Qt paths and connect each kit to the matching host Qt.",
	} {
		if !bytes.Contains(output.Bytes(), []byte(want)) {
			t.Errorf("output does not contain %q:\n%s", want, output)
		}
	}
}

func TestInstallQtDesktopDryRunUsesNativeArchitecture(t *testing.T) {
	output := &bytes.Buffer{}
	root, err := filepath.Abs(filepath.Join(".tmp", "desktop-cli-root"))
	if err != nil {
		t.Fatalf("filepath.Abs() error = %v", err)
	}
	kitDir := filepath.Join(root, "6.11.2", "macos")
	client := &stubRepositoryClient{
		installPlan: qtrepo.InstallPlan{
			Version: qtrepo.Version{Major: 6, Minor: 11, Patch: 2},
			Host:    qtrepo.HostMac,
			Target:  qtrepo.TargetDesktop,
			DesktopKit: &qtrepo.DesktopKit{
				Architecture: qtrepo.DesktopArchitectureMacClang64,
				Destination:  kitDir,
				Packages: []qtrepo.PackageSelection{
					{
						Name:           "qt.qt6.6112.clang_64",
						PackageVersion: "6.11.2-0-test",
						Archives: []qtrepo.Archive{
							{
								Name:      "qtbase",
								URL:       "https://mirror.example/qtbase.7z",
								ExtractTo: kitDir,
								Checksum: qtrepo.Checksum{
									Algorithm: qtrepo.ChecksumSHA256,
									URL:       "https://mirror.example/qtbase.7z.sha256",
								},
							},
						},
					},
				},
			},
		},
	}
	command := newCommand(
		commandDependencies{
			install: installCommandDependencies{resolver: client},
		},
		qtrepo.HostMac,
		output,
		&bytes.Buffer{},
	)

	err = command.Run(context.Background(), []string{
		"yaqt",
		"install-qt",
		"6.11.2",
		"--target", "desktop",
		"--root", root,
		"--dry-run",
	})
	if err != nil {
		t.Fatalf("command.Run() error = %v", err)
	}

	request := client.installRequest
	if got, want := request.DesktopArchitecture, qtrepo.DesktopArchitectureMacClang64; got != want {
		t.Errorf("DesktopArchitecture = %q, want %q", got, want)
	}
	if len(request.AndroidABIs) != 0 {
		t.Errorf("AndroidABIs = %v, want none", request.AndroidABIs)
	}
	for _, want := range []string{
		"Qt 6.11.2 for desktop on mac",
		"clang_64 -> " + kitDir,
		"base package: qt.qt6.6112.clang_64",
		"Post-install: relocate desktop Qt paths.",
	} {
		if !bytes.Contains(output.Bytes(), []byte(want)) {
			t.Errorf("output does not contain %q:\n%s", want, output)
		}
	}
	if bytes.Contains(output.Bytes(), []byte("Host Qt requirement:")) {
		t.Errorf("desktop plan unexpectedly contains a host Qt requirement:\n%s", output)
	}
}

func TestInstallQtIOSDryRun(t *testing.T) {
	output := &bytes.Buffer{}
	root, err := filepath.Abs(filepath.Join(".tmp", "ios-cli-root"))
	if err != nil {
		t.Fatalf("filepath.Abs() error = %v", err)
	}
	kitDir := filepath.Join(root, "6.11.2", "ios")
	client := &stubRepositoryClient{
		installPlan: qtrepo.InstallPlan{
			Version: qtrepo.Version{Major: 6, Minor: 11, Patch: 2},
			Host:    qtrepo.HostMac,
			Target:  qtrepo.TargetIOS,
			HostQt: &qtrepo.QtInstallationIdentity{
				Host:    qtrepo.HostMac,
				Version: qtrepo.Version{Major: 6, Minor: 11, Patch: 2},
			},
			IOSKit: &qtrepo.IOSKit{
				Destination: kitDir,
				Packages: []qtrepo.PackageSelection{
					{
						Name:           "qt.qt6.6112.ios",
						PackageVersion: "6.11.2-0-test",
						Archives: []qtrepo.Archive{
							{
								Name:      "qtbase",
								URL:       "https://mirror.example/qtbase.7z",
								ExtractTo: kitDir,
								Checksum: qtrepo.Checksum{
									Algorithm: qtrepo.ChecksumSHA256,
									URL:       "https://mirror.example/qtbase.7z.sha256",
								},
							},
						},
					},
				},
			},
		},
	}
	command := newCommand(
		commandDependencies{
			install: installCommandDependencies{resolver: client},
		},
		qtrepo.HostMac,
		output,
		&bytes.Buffer{},
	)

	err = command.Run(context.Background(), []string{
		"yaqt",
		"install-qt",
		"6.11.2",
		"--target", "ios",
		"--root", root,
		"--dry-run",
	})
	if err != nil {
		t.Fatalf("command.Run() error = %v", err)
	}

	request := client.installRequest
	if request.DesktopArchitecture != "" {
		t.Errorf("DesktopArchitecture = %q, want none", request.DesktopArchitecture)
	}
	if len(request.AndroidABIs) != 0 {
		t.Errorf("AndroidABIs = %v, want none", request.AndroidABIs)
	}
	for _, want := range []string{
		"Qt 6.11.2 for ios on mac",
		"Host Qt requirement: mac desktop 6.11.2",
		"ios -> " + kitDir,
		"base package: qt.qt6.6112.ios",
		"Post-install: relocate iOS Qt paths and connect the kit to the matching host Qt.",
	} {
		if !bytes.Contains(output.Bytes(), []byte(want)) {
			t.Errorf("output does not contain %q:\n%s", want, output)
		}
	}
}

func TestInstallQtIOSRejectsArchitectureFlags(t *testing.T) {
	for _, flag := range [][]string{
		{"--arch", "clang_64"},
		{"--abi", "arm64-v8a"},
	} {
		t.Run(flag[0], func(t *testing.T) {
			command := newCommand(
				commandDependencies{
					install: installCommandDependencies{resolver: &stubRepositoryClient{}},
				},
				qtrepo.HostMac,
				&bytes.Buffer{},
				&bytes.Buffer{},
			)
			args := []string{
				"yaqt", "install-qt", "6.11.2",
				"--target", "ios",
				"--root", filepath.Join(".tmp", "Qt"),
				"--dry-run",
			}
			args = append(args, flag...)
			if err := command.Run(context.Background(), args); err == nil {
				t.Fatalf("command.Run() error = nil, want %s to be rejected", flag[0])
			}
		})
	}
}

func TestInstallQtRejectsTargetSpecificArchitectureFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "desktop ABI",
			args: []string{"--target", "desktop", "--abi", "arm64-v8a"},
			want: "--abi can be used only with --target android",
		},
		{
			name: "Android desktop architecture",
			args: []string{"--target", "android", "--abi", "arm64-v8a", "--arch", "linux_gcc_64"},
			want: "--arch can be used only with --target desktop",
		},
		{
			name: "Android without ABI",
			args: []string{"--target", "android"},
			want: "at least one --abi is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &stubRepositoryClient{}
			command := newCommand(
				commandDependencies{
					install: installCommandDependencies{resolver: client},
				},
				qtrepo.HostLinux,
				&bytes.Buffer{},
				&bytes.Buffer{},
			)
			args := []string{"yaqt", "install-qt", "6.8.0", "--root", filepath.Join(".tmp", "Qt"), "--dry-run"}
			args = append(args, test.args...)
			err := command.Run(context.Background(), args)
			if err == nil || !bytes.Contains([]byte(err.Error()), []byte(test.want)) {
				t.Fatalf("command.Run() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestInstallQtDownloadOnlyUsesExplicitCacheDirectory(t *testing.T) {
	output := &bytes.Buffer{}
	archive := qtrepo.Archive{
		Name: "qtbase",
		URL:  "https://mirror.example/qtbase.7z",
		Checksum: qtrepo.Checksum{
			Algorithm: qtrepo.ChecksumSHA256,
			URL:       "https://mirror.example/qtbase.7z.sha256",
		},
	}
	client := &stubRepositoryClient{
		installPlan: qtrepo.InstallPlan{
			Version: qtrepo.Version{Major: 6, Minor: 8},
			Host:    qtrepo.HostLinux,
			Target:  qtrepo.TargetAndroid,
			AndroidKits: []qtrepo.AndroidKit{
				{
					Packages: []qtrepo.PackageSelection{
						{Name: "qt.qt6.680.android_arm64_v8a", Archives: []qtrepo.Archive{archive}},
					},
				},
			},
		},
	}
	cacheDir := filepath.Join(".tmp", "manual-cache")
	fetcher := &stubArchiveFetcher{destination: filepath.Join(cacheDir, "downloads", "sha256")}
	var factoryCacheDir string
	factory := archiveFetcherFactory(func(resolvedCacheDir string) (archiveFetcher, error) {
		factoryCacheDir = resolvedCacheDir
		return fetcher, nil
	})
	command := newCommand(
		commandDependencies{
			install: installCommandDependencies{
				resolver:       client,
				fetcherFactory: factory,
			},
		},
		qtrepo.HostLinux,
		output,
		&bytes.Buffer{},
	)

	err := command.Run(context.Background(), []string{
		"yaqt",
		"install-qt",
		"6.8.0",
		"--target", "android",
		"--abi", "arm64-v8a",
		"--root", filepath.Join(".tmp", "Qt"),
		"--download-only",
		"--cache-dir", cacheDir,
	})
	if err != nil {
		t.Fatalf("command.Run() error = %v", err)
	}
	if got, want := factoryCacheDir, filepath.Clean(cacheDir); got != want {
		t.Errorf("archive fetcher cache directory = %q, want %q", got, want)
	}
	if got, want := client.cacheRoot, filepath.Clean(cacheDir); got != want {
		t.Errorf("repository cache root = %q, want %q", got, want)
	}
	if got, want := fetcher.archives, []qtrepo.Archive{archive}; len(got) != len(want) || got[0] != want[0] {
		t.Errorf("fetched archives = %v, want %v", got, want)
	}
	for _, want := range []string{
		"Cache: " + filepath.Clean(cacheDir),
		"Cached qtbase: " + filepath.Join(fetcher.destination, "qtbase.7z"),
	} {
		if !bytes.Contains(output.Bytes(), []byte(want)) {
			t.Errorf("output does not contain %q:\n%s", want, output)
		}
	}
}

func TestInstallQtExtractOnlyDownloadsAndExtractsArchives(t *testing.T) {
	output := &bytes.Buffer{}
	extractTo := filepath.Join(".tmp", "Qt", "6.8.0", "android_arm64_v8a")
	archive := qtrepo.Archive{
		Name:      "qtbase",
		URL:       "https://mirror.example/qtbase.7z",
		ExtractTo: extractTo,
		Checksum: qtrepo.Checksum{
			Algorithm: qtrepo.ChecksumSHA256,
			URL:       "https://mirror.example/qtbase.7z.sha256",
		},
	}
	client := &stubRepositoryClient{
		installPlan: qtrepo.InstallPlan{
			Version: qtrepo.Version{Major: 6, Minor: 8},
			Host:    qtrepo.HostLinux,
			Target:  qtrepo.TargetAndroid,
			AndroidKits: []qtrepo.AndroidKit{
				{Packages: []qtrepo.PackageSelection{{Name: "qt.qt6.680.android_arm64_v8a", Archives: []qtrepo.Archive{archive}}}},
			},
		},
	}
	cacheDir := filepath.Join(".tmp", "manual-cache")
	fetcher := &stubArchiveFetcher{destination: filepath.Join(cacheDir, "downloads", "sha256")}
	factory := archiveFetcherFactory(func(string) (archiveFetcher, error) {
		return fetcher, nil
	})
	extractor := &stubArchiveExtractor{}
	command := newCommand(
		commandDependencies{
			install: installCommandDependencies{
				resolver:       client,
				fetcherFactory: factory,
				extractor:      extractor,
			},
		},
		qtrepo.HostLinux,
		output,
		&bytes.Buffer{},
	)

	err := command.Run(context.Background(), []string{
		"yaqt",
		"install-qt",
		"6.8.0",
		"--target", "android",
		"--abi", "arm64-v8a",
		"--root", filepath.Join(".tmp", "Qt"),
		"--extract-only",
		"--cache-dir", cacheDir,
	})
	if err != nil {
		t.Fatalf("command.Run() error = %v", err)
	}
	wantArchivePath := filepath.Join(fetcher.destination, "qtbase.7z")
	wantExtractions := []archiveExtraction{{archivePath: wantArchivePath, destination: extractTo}}
	if got := extractor.extractions; len(got) != len(wantExtractions) || got[0] != wantExtractions[0] {
		t.Errorf("extractions = %v, want %v", got, wantExtractions)
	}
	for _, want := range []string{
		"Cached qtbase: " + wantArchivePath,
		"Extracted qtbase to " + extractTo,
		"Path relocation has not been applied.",
	} {
		if !bytes.Contains(output.Bytes(), []byte(want)) {
			t.Errorf("output does not contain %q:\n%s", want, output)
		}
	}
}

func TestInstallQtExtractOnlyDownloadsEveryArchiveBeforeExtraction(t *testing.T) {
	archives := []qtrepo.Archive{
		{Name: "qtbase", ExtractTo: filepath.Join(".tmp", "Qt")},
		{Name: "qtsvg", ExtractTo: filepath.Join(".tmp", "Qt")},
	}
	client := &stubRepositoryClient{
		installPlan: qtrepo.InstallPlan{
			Version: qtrepo.Version{Major: 6, Minor: 8},
			Host:    qtrepo.HostLinux,
			Target:  qtrepo.TargetAndroid,
			AndroidKits: []qtrepo.AndroidKit{
				{Packages: []qtrepo.PackageSelection{{Archives: archives}}},
			},
		},
	}
	fetcher := &stubArchiveFetcher{destination: filepath.Join(".tmp", "cache"), failOn: "qtsvg"}
	factory := archiveFetcherFactory(func(string) (archiveFetcher, error) {
		return fetcher, nil
	})
	extractor := &stubArchiveExtractor{}
	command := newCommand(
		commandDependencies{
			install: installCommandDependencies{
				resolver:       client,
				fetcherFactory: factory,
				extractor:      extractor,
			},
		},
		qtrepo.HostLinux,
		&bytes.Buffer{},
		&bytes.Buffer{},
	)

	err := command.Run(context.Background(), []string{
		"yaqt",
		"install-qt",
		"6.8.0",
		"--target", "android",
		"--abi", "arm64-v8a",
		"--root", filepath.Join(".tmp", "Qt"),
		"--extract-only",
	})
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("download failed")) {
		t.Fatalf("command.Run() error = %v, want a download error", err)
	}
	if len(extractor.extractions) != 0 {
		t.Errorf("extractions = %v, want none before every archive is cached", extractor.extractions)
	}
}

func TestInstallQtCompleteDownloadsExtractsAndRelocates(t *testing.T) {
	events := make([]string, 0)
	output := &bytes.Buffer{}
	qtRoot := cleanInstallTestRoot(t, "android-complete")
	kitDir := filepath.Join(qtRoot, "6.8.0", "android_arm64_v8a")
	archive := qtrepo.Archive{
		Name:      "qtbase",
		ExtractTo: kitDir,
	}
	client := &stubRepositoryClient{
		installPlan: qtrepo.InstallPlan{
			Version: qtrepo.Version{Major: 6, Minor: 8},
			Host:    qtrepo.HostLinux,
			Target:  qtrepo.TargetAndroid,
			HostQt: &qtrepo.QtInstallationIdentity{
				Host:    qtrepo.HostLinux,
				Version: qtrepo.Version{Major: 6, Minor: 8},
			},
			AndroidKits: []qtrepo.AndroidKit{
				{
					ABI:         qtrepo.AndroidABIArm64V8A,
					Destination: kitDir,
					Packages: []qtrepo.PackageSelection{
						{
							Name:           "qt.qt6.680.android_arm64_v8a",
							PackageVersion: "6.8.0-0-test",
							Archives:       []qtrepo.Archive{archive},
						},
					},
				},
			},
		},
	}
	cacheDir := filepath.Join(qtRoot, "cache")
	fetcher := &stubArchiveFetcher{
		destination: filepath.Join(cacheDir, "downloads"),
		events:      &events,
	}
	factory := archiveFetcherFactory(func(string) (archiveFetcher, error) {
		return fetcher, nil
	})
	extractor := &stubArchiveExtractor{events: &events}
	relocator := &stubInstallRelocator{events: &events}
	var configuredPlan qtrepo.InstallPlan
	var configuredRoot string
	relocatorFactory := installRelocatorFactory(func(
		plan qtrepo.InstallPlan,
		root string,
	) (installRelocator, error) {
		configuredPlan = plan
		configuredRoot = root
		return relocator, nil
	})
	command := newCommand(
		commandDependencies{
			install: installCommandDependencies{
				resolver:         client,
				fetcherFactory:   factory,
				extractor:        extractor,
				relocatorFactory: relocatorFactory,
			},
		},
		qtrepo.HostLinux,
		output,
		&bytes.Buffer{},
	)

	err := command.Run(context.Background(), []string{
		"yaqt",
		"install-qt",
		"6.8.0",
		"--target", "android",
		"--abi", "arm64-v8a",
		"--root", qtRoot,
		"--cache-dir", cacheDir,
	})
	if err != nil {
		t.Fatalf("command.Run() error = %v", err)
	}
	if configuredPlan.HostQt == nil || client.installPlan.HostQt == nil ||
		*configuredPlan.HostQt != *client.installPlan.HostQt || configuredRoot != qtRoot {
		t.Errorf(
			"relocation configuration = requirement %+v, root %q; want requirement %+v, root %q",
			configuredPlan.HostQt,
			configuredRoot,
			client.installPlan.HostQt,
			qtRoot,
		)
	}
	if got, want := relocator.kitDirs, []string{kitDir}; len(got) != len(want) || got[0] != want[0] {
		t.Errorf("relocated kit directories = %v, want %v", got, want)
	}
	wantEvents := []string{"fetch qtbase", "extract qtbase.7z", "relocate android_arm64_v8a"}
	if len(events) != len(wantEvents) {
		t.Fatalf("installation events = %v, want %v", events, wantEvents)
	}
	for index, want := range wantEvents {
		if events[index] != want {
			t.Errorf("installation event %d = %q, want %q", index, events[index], want)
		}
	}
	for _, want := range []string{
		"Cached qtbase:",
		"Extracted qtbase to " + kitDir,
		"Relocated arm64-v8a kit: " + kitDir,
		"Installed Qt 6.8.0 for android.",
	} {
		if !bytes.Contains(output.Bytes(), []byte(want)) {
			t.Errorf("output does not contain %q:\n%s", want, output)
		}
	}
}

func TestInstallQtDesktopCompleteDownloadsExtractsAndRelocates(t *testing.T) {
	events := make([]string, 0)
	output := &bytes.Buffer{}
	qtRoot := cleanInstallTestRoot(t, "desktop-complete")
	kitDir := filepath.Join(qtRoot, "6.11.2", "macos")
	archive := qtrepo.Archive{Name: "qtbase", ExtractTo: kitDir}
	client := &stubRepositoryClient{
		installPlan: qtrepo.InstallPlan{
			Version: qtrepo.Version{Major: 6, Minor: 11, Patch: 2},
			Host:    qtrepo.HostMac,
			Target:  qtrepo.TargetDesktop,
			DesktopKit: &qtrepo.DesktopKit{
				Architecture: qtrepo.DesktopArchitectureMacClang64,
				Destination:  kitDir,
				Packages: []qtrepo.PackageSelection{
					{
						Name:           "qt.qt6.6112.clang_64",
						PackageVersion: "6.11.2-0-test",
						Archives:       []qtrepo.Archive{archive},
					},
				},
			},
		},
	}
	cacheDir := filepath.Join(qtRoot, "cache")
	fetcher := &stubArchiveFetcher{
		destination: filepath.Join(cacheDir, "downloads"),
		events:      &events,
	}
	extractor := &stubArchiveExtractor{events: &events}
	relocator := &stubInstallRelocator{events: &events}
	var configuredTarget qtrepo.Target
	command := newCommand(
		commandDependencies{
			install: installCommandDependencies{
				resolver: client,
				fetcherFactory: archiveFetcherFactory(func(string) (archiveFetcher, error) {
					return fetcher, nil
				}),
				extractor: extractor,
				relocatorFactory: installRelocatorFactory(func(
					plan qtrepo.InstallPlan,
					_ string,
				) (installRelocator, error) {
					configuredTarget = plan.Target
					return relocator, nil
				}),
			},
		},
		qtrepo.HostMac,
		output,
		&bytes.Buffer{},
	)

	err := command.Run(context.Background(), []string{
		"yaqt",
		"install-qt",
		"6.11.2",
		"--target", "desktop",
		"--root", qtRoot,
		"--cache-dir", cacheDir,
	})
	if err != nil {
		t.Fatalf("command.Run() error = %v", err)
	}
	if configuredTarget != qtrepo.TargetDesktop {
		t.Errorf("relocator target = %q, want %q", configuredTarget, qtrepo.TargetDesktop)
	}
	if got, want := relocator.kitDirs, []string{kitDir}; len(got) != len(want) || got[0] != want[0] {
		t.Errorf("relocated kit directories = %v, want %v", got, want)
	}
	wantEvents := []string{"fetch qtbase", "extract qtbase.7z", "relocate macos"}
	if len(events) != len(wantEvents) {
		t.Fatalf("installation events = %v, want %v", events, wantEvents)
	}
	for index, want := range wantEvents {
		if events[index] != want {
			t.Errorf("installation event %d = %q, want %q", index, events[index], want)
		}
	}
	for _, want := range []string{
		"Extracted qtbase to " + kitDir,
		"Relocated clang_64 kit: " + kitDir,
		"Installed Qt 6.11.2 for desktop.",
	} {
		if !bytes.Contains(output.Bytes(), []byte(want)) {
			t.Errorf("output does not contain %q:\n%s", want, output)
		}
	}
}

func TestInstallQtRepeatingSatisfiedInstallationSkipsWork(t *testing.T) {
	qtRoot := cleanInstallTestRoot(t, "repeat-satisfied")
	kitDir := filepath.Join(qtRoot, "6.11.2", "macos")
	archive := qtrepo.Archive{Name: "qtbase", ExtractTo: kitDir}
	client := &stubRepositoryClient{
		installPlan: qtrepo.InstallPlan{
			Version: qtrepo.Version{Major: 6, Minor: 11, Patch: 2},
			Host:    qtrepo.HostMac,
			Target:  qtrepo.TargetDesktop,
			DesktopKit: &qtrepo.DesktopKit{
				Architecture: qtrepo.DesktopArchitectureMacClang64,
				Destination:  kitDir,
				Packages: []qtrepo.PackageSelection{{
					Name:           "qt.qt6.6112.clang_64",
					PackageVersion: "6.11.2-0-202608131016",
					Archives:       []qtrepo.Archive{archive},
				}},
			},
		},
	}
	events := make([]string, 0)
	cacheDir := filepath.Join(qtRoot, "cache")
	dependencies := commandDependencies{
		install: installCommandDependencies{
			resolver: client,
			fetcherFactory: archiveFetcherFactory(func(string) (archiveFetcher, error) {
				return &stubArchiveFetcher{destination: cacheDir, events: &events}, nil
			}),
			extractor: &stubArchiveExtractor{events: &events},
			relocatorFactory: installRelocatorFactory(func(
				qtrepo.InstallPlan,
				string,
			) (installRelocator, error) {
				return &stubInstallRelocator{events: &events}, nil
			}),
		},
	}
	arguments := []string{
		"yaqt", "install-qt", "6.11.2",
		"--target", "desktop",
		"--root", qtRoot,
		"--cache-dir", cacheDir,
	}

	firstOutput := &bytes.Buffer{}
	if err := newCommand(
		dependencies,
		qtrepo.HostMac,
		firstOutput,
		&bytes.Buffer{},
	).Run(context.Background(), arguments); err != nil {
		t.Fatalf("first command.Run() error = %v", err)
	}
	if got, want := len(events), 3; got != want {
		t.Fatalf("first installation event count = %d, want %d: %v", got, want, events)
	}

	secondOutput := &bytes.Buffer{}
	if err := newCommand(
		dependencies,
		qtrepo.HostMac,
		secondOutput,
		&bytes.Buffer{},
	).Run(context.Background(), arguments); err != nil {
		t.Fatalf("second command.Run() error = %v", err)
	}
	if got, want := len(events), 3; got != want {
		t.Errorf("event count after repeated installation = %d, want %d: %v", got, want, events)
	}
	if got := secondOutput.String(); !bytes.Contains(
		[]byte(got),
		[]byte("Qt 6.11.2 for desktop is already satisfied."),
	) {
		t.Errorf("second output does not report a satisfied installation:\n%s", got)
	}

	dryRunOutput := &bytes.Buffer{}
	dryRunArguments := append(append([]string(nil), arguments...), "--dry-run")
	if err := newCommand(
		dependencies,
		qtrepo.HostMac,
		dryRunOutput,
		&bytes.Buffer{},
	).Run(context.Background(), dryRunArguments); err != nil {
		t.Fatalf("dry-run command.Run() error = %v", err)
	}
	if got := dryRunOutput.String(); !bytes.Contains(
		[]byte(got),
		[]byte("Qt 6.11.2 for desktop is already satisfied."),
	) {
		t.Errorf("dry-run output does not report a satisfied installation:\n%s", got)
	}
	if got, want := len(events), 3; got != want {
		t.Errorf("event count after satisfied dry run = %d, want %d: %v", got, want, events)
	}
}

func TestInstallQtAdoptsLegacyBaseBeforeAddingModule(t *testing.T) {
	qtRoot := cleanInstallTestRoot(t, "adopt-legacy-base")
	kitDir := filepath.Join(qtRoot, "6.11.2", "macos")
	if err := os.MkdirAll(kitDir, 0o755); err != nil {
		t.Fatalf("create legacy kit: %v", err)
	}
	baseArchive := qtrepo.Archive{Name: "qtbase", ExtractTo: kitDir}
	moduleArchive := qtrepo.Archive{Name: "qtmultimedia", ExtractTo: kitDir}
	client := &stubRepositoryClient{
		installPlan: qtrepo.InstallPlan{
			Version: qtrepo.Version{Major: 6, Minor: 11, Patch: 2},
			Host:    qtrepo.HostMac,
			Target:  qtrepo.TargetDesktop,
			DesktopKit: &qtrepo.DesktopKit{
				Architecture: qtrepo.DesktopArchitectureMacClang64,
				Destination:  kitDir,
				Packages: []qtrepo.PackageSelection{
					{
						Name:           "qt.qt6.6112.clang_64",
						PackageVersion: "6.11.2-0-202608131016",
						Archives:       []qtrepo.Archive{baseArchive},
					},
					{
						Name:           "qt.qt6.6112.addons.qtmultimedia.clang_64",
						PackageVersion: "6.11.2-0-202608131016",
						Module:         "qtmultimedia",
						Archives:       []qtrepo.Archive{moduleArchive},
					},
				},
			},
		},
	}
	events := make([]string, 0)
	cacheDir := filepath.Join(qtRoot, "cache")
	command := newCommand(
		commandDependencies{
			install: installCommandDependencies{
				resolver: client,
				fetcherFactory: archiveFetcherFactory(func(string) (archiveFetcher, error) {
					return &stubArchiveFetcher{destination: cacheDir, events: &events}, nil
				}),
				extractor: &stubArchiveExtractor{events: &events},
				relocatorFactory: installRelocatorFactory(func(
					qtrepo.InstallPlan,
					string,
				) (installRelocator, error) {
					return &stubInstallRelocator{events: &events}, nil
				}),
			},
		},
		qtrepo.HostMac,
		&bytes.Buffer{},
		&bytes.Buffer{},
	)

	if err := command.Run(context.Background(), []string{
		"yaqt", "install-qt", "6.11.2",
		"--target", "desktop",
		"--root", qtRoot,
		"--cache-dir", cacheDir,
		"--module", "qtmultimedia",
	}); err != nil {
		t.Fatalf("command.Run() error = %v", err)
	}
	wantEvents := []string{
		"fetch qtmultimedia",
		"extract qtmultimedia.7z",
		"relocate macos",
	}
	if got := events; !slices.Equal(got, wantEvents) {
		t.Errorf("installation events = %v, want %v", got, wantEvents)
	}
}

func TestInstallQtDryRunReportsIncrementalPackageActions(t *testing.T) {
	qtRoot := cleanInstallTestRoot(t, "dry-run-actions")
	kitDir := filepath.Join(qtRoot, "6.11.2", "macos")
	basePackage := qtrepo.PackageSelection{
		Name:           "qt.qt6.6112.clang_64",
		PackageVersion: "6.11.2-0-202608131016",
		Archives: []qtrepo.Archive{{
			Name:      "qtbase",
			ExtractTo: kitDir,
		}},
	}
	client := &stubRepositoryClient{
		installPlan: qtrepo.InstallPlan{
			Version: qtrepo.Version{Major: 6, Minor: 11, Patch: 2},
			Host:    qtrepo.HostMac,
			Target:  qtrepo.TargetDesktop,
			DesktopKit: &qtrepo.DesktopKit{
				Architecture: qtrepo.DesktopArchitectureMacClang64,
				Destination:  kitDir,
				Packages:     []qtrepo.PackageSelection{basePackage},
			},
		},
	}
	events := make([]string, 0)
	cacheDir := filepath.Join(qtRoot, "cache")
	installDependencies := installCommandDependencies{
		resolver: client,
		fetcherFactory: archiveFetcherFactory(func(string) (archiveFetcher, error) {
			return &stubArchiveFetcher{destination: cacheDir, events: &events}, nil
		}),
		extractor: &stubArchiveExtractor{events: &events},
		relocatorFactory: installRelocatorFactory(func(
			qtrepo.InstallPlan,
			string,
		) (installRelocator, error) {
			return &stubInstallRelocator{events: &events}, nil
		}),
	}
	if err := newCommand(
		commandDependencies{install: installDependencies},
		qtrepo.HostMac,
		&bytes.Buffer{},
		&bytes.Buffer{},
	).Run(context.Background(), []string{
		"yaqt", "install-qt", "6.11.2",
		"--target", "desktop",
		"--root", qtRoot,
		"--cache-dir", cacheDir,
	}); err != nil {
		t.Fatalf("initial command.Run() error = %v", err)
	}

	client.installPlan.DesktopKit.Packages = append(
		client.installPlan.DesktopKit.Packages,
		qtrepo.PackageSelection{
			Name:           "qt.qt6.6112.addons.qtmultimedia.clang_64",
			PackageVersion: "6.11.2-0-202608131016",
			Module:         "qtmultimedia",
			Archives: []qtrepo.Archive{{
				Name:      "qtmultimedia",
				ExtractTo: kitDir,
			}},
		},
	)
	dryRunOutput := &bytes.Buffer{}
	if err := newCommand(
		commandDependencies{install: installDependencies},
		qtrepo.HostMac,
		dryRunOutput,
		&bytes.Buffer{},
	).Run(context.Background(), []string{
		"yaqt", "install-qt", "6.11.2",
		"--target", "desktop",
		"--root", qtRoot,
		"--module", "qtmultimedia",
		"--dry-run",
	}); err != nil {
		t.Fatalf("dry-run command.Run() error = %v", err)
	}

	for _, want := range []string{
		"base package: qt.qt6.6112.clang_64\n    action: skip",
		"module qtmultimedia: qt.qt6.6112.addons.qtmultimedia.clang_64\n    action: install",
	} {
		if !bytes.Contains(dryRunOutput.Bytes(), []byte(want)) {
			t.Errorf("dry-run output does not contain %q:\n%s", want, dryRunOutput)
		}
	}
	if got, want := len(events), 3; got != want {
		t.Errorf("event count after dry run = %d, want %d: %v", got, want, events)
	}

	client.installPlan.DesktopKit.Packages[0].PackageVersion = "6.11.2-1-updated"
	updateOutput := &bytes.Buffer{}
	if err := newCommand(
		commandDependencies{install: installDependencies},
		qtrepo.HostMac,
		updateOutput,
		&bytes.Buffer{},
	).Run(context.Background(), []string{
		"yaqt", "install-qt", "6.11.2",
		"--target", "desktop",
		"--root", qtRoot,
		"--dry-run",
	}); err != nil {
		t.Fatalf("update dry-run command.Run() error = %v", err)
	}
	if !bytes.Contains(
		updateOutput.Bytes(),
		[]byte("base package: qt.qt6.6112.clang_64\n    action: update"),
	) {
		t.Errorf("update dry-run output does not report the changed base package:\n%s", updateOutput)
	}
}

func TestInstallQtSerializesDifferentKitsOfTheSameVersion(t *testing.T) {
	qtRoot := cleanInstallTestRoot(t, "same-version-lock")
	version := qtrepo.Version{Major: 6, Minor: 11, Patch: 2}
	firstRelease := make(chan struct{})
	secondRelease := make(chan struct{})
	firstEntered := make(chan struct{}, 1)
	secondEntered := make(chan struct{}, 1)

	newInstallCommand := func(
		plan qtrepo.InstallPlan,
		fetcher archiveFetcher,
		output *bytes.Buffer,
	) *cli.Command {
		return newCommand(
			commandDependencies{
				install: installCommandDependencies{
					resolver: &stubRepositoryClient{installPlan: plan},
					fetcherFactory: archiveFetcherFactory(func(string) (archiveFetcher, error) {
						return fetcher, nil
					}),
					extractor: &stubArchiveExtractor{},
					relocatorFactory: installRelocatorFactory(func(
						qtrepo.InstallPlan,
						string,
					) (installRelocator, error) {
						return &stubInstallRelocator{}, nil
					}),
				},
			},
			qtrepo.HostMac,
			output,
			&bytes.Buffer{},
		)
	}

	desktopKitDir := filepath.Join(qtRoot, version.String(), "macos")
	desktopPlan := qtrepo.InstallPlan{
		Version: version,
		Host:    qtrepo.HostMac,
		Target:  qtrepo.TargetDesktop,
		DesktopKit: &qtrepo.DesktopKit{
			Architecture: qtrepo.DesktopArchitectureMacClang64,
			Destination:  desktopKitDir,
			Packages: []qtrepo.PackageSelection{{
				Name:           "qt.qt6.6112.clang_64",
				PackageVersion: "6.11.2-0-test",
				Archives: []qtrepo.Archive{{
					Name:      "qtbase",
					ExtractTo: desktopKitDir,
				}},
			}},
		},
	}
	iosKitDir := filepath.Join(qtRoot, version.String(), "ios")
	iosPlan := qtrepo.InstallPlan{
		Version: version,
		Host:    qtrepo.HostMac,
		Target:  qtrepo.TargetIOS,
		HostQt: &qtrepo.QtInstallationIdentity{
			Host:    qtrepo.HostMac,
			Version: version,
		},
		IOSKit: &qtrepo.IOSKit{
			Destination: iosKitDir,
			Packages: []qtrepo.PackageSelection{{
				Name:           "qt.qt6.6112.ios",
				PackageVersion: "6.11.2-0-test",
				Archives: []qtrepo.Archive{{
					Name:      "qtbase",
					ExtractTo: iosKitDir,
				}},
			}},
		},
	}

	firstOutput := &bytes.Buffer{}
	secondOutput := &bytes.Buffer{}
	firstCommand := newInstallCommand(desktopPlan, &blockingArchiveFetcher{
		destination: filepath.Join(qtRoot, "first-cache"),
		entered:     firstEntered,
		release:     firstRelease,
	}, firstOutput)
	secondCommand := newInstallCommand(iosPlan, &blockingArchiveFetcher{
		destination: filepath.Join(qtRoot, "second-cache"),
		entered:     secondEntered,
		release:     secondRelease,
	}, secondOutput)
	firstResult := make(chan error, 1)
	secondResult := make(chan error, 1)
	go func() {
		firstResult <- firstCommand.Run(context.Background(), []string{
			"yaqt", "install-qt", version.String(),
			"--target", "desktop",
			"--root", qtRoot,
		})
	}()
	select {
	case <-firstEntered:
	case <-time.After(2 * time.Second):
		close(firstRelease)
		t.Fatal("first installation did not start downloading")
	}

	go func() {
		secondResult <- secondCommand.Run(context.Background(), []string{
			"yaqt", "install-qt", version.String(),
			"--target", "ios",
			"--root", qtRoot,
		})
	}()
	select {
	case <-secondEntered:
		close(firstRelease)
		close(secondRelease)
		<-firstResult
		<-secondResult
		t.Fatal("second kit started downloading while the same Qt version was being installed")
	case <-time.After(100 * time.Millisecond):
	}

	close(firstRelease)
	if err := <-firstResult; err != nil {
		close(secondRelease)
		<-secondResult
		t.Fatalf("first command.Run() error = %v", err)
	}
	select {
	case <-secondEntered:
	case <-time.After(2 * time.Second):
		close(secondRelease)
		<-secondResult
		t.Fatal("second installation did not resume after the first released the version lock")
	}
	close(secondRelease)
	if err := <-secondResult; err != nil {
		t.Fatalf("second command.Run() error = %v", err)
	}
	if !bytes.Contains(
		secondOutput.Bytes(),
		[]byte("Waiting for another installation of Qt 6.11.2 to finish."),
	) {
		t.Errorf("second output does not report the version lock wait:\n%s", secondOutput)
	}
}

func TestInstallQtPublishesManifestsOnlyAfterAllKitsRelocate(t *testing.T) {
	qtRoot := cleanInstallTestRoot(t, "publish-manifests-after-relocation")
	version := qtrepo.Version{Major: 6, Minor: 11, Patch: 2}
	armKitDir := filepath.Join(qtRoot, version.String(), "android_arm64_v8a")
	x86KitDir := filepath.Join(qtRoot, version.String(), "android_x86_64")
	plan := qtrepo.InstallPlan{
		Version: version,
		Host:    qtrepo.HostMac,
		Target:  qtrepo.TargetAndroid,
		AndroidKits: []qtrepo.AndroidKit{
			{
				ABI:         qtrepo.AndroidABIArm64V8A,
				Destination: armKitDir,
				Packages: []qtrepo.PackageSelection{{
					Name:           "qt.qt6.6112.android_arm64_v8a",
					PackageVersion: "6.11.2-0-test",
					Archives: []qtrepo.Archive{{
						Name:      "qtbase-arm64",
						ExtractTo: armKitDir,
					}},
				}},
			},
			{
				ABI:         qtrepo.AndroidABIX8664,
				Destination: x86KitDir,
				Packages: []qtrepo.PackageSelection{{
					Name:           "qt.qt6.6112.android_x86_64",
					PackageVersion: "6.11.2-0-test",
					Archives: []qtrepo.Archive{{
						Name:      "qtbase-x86_64",
						ExtractTo: x86KitDir,
					}},
				}},
			},
		},
	}
	events := make([]string, 0)
	cacheDir := filepath.Join(qtRoot, "cache")
	command := newCommand(
		commandDependencies{
			install: installCommandDependencies{
				resolver: &stubRepositoryClient{installPlan: plan},
				fetcherFactory: archiveFetcherFactory(func(string) (archiveFetcher, error) {
					return &stubArchiveFetcher{destination: cacheDir, events: &events}, nil
				}),
				extractor: &stubArchiveExtractor{events: &events},
				relocatorFactory: installRelocatorFactory(func(
					qtrepo.InstallPlan,
					string,
				) (installRelocator, error) {
					return &stubInstallRelocator{
						events: &events,
						failOn: filepath.Base(x86KitDir),
					}, nil
				}),
			},
		},
		qtrepo.HostMac,
		&bytes.Buffer{},
		&bytes.Buffer{},
	)

	err := command.Run(context.Background(), []string{
		"yaqt", "install-qt", version.String(),
		"--target", "android",
		"--root", qtRoot,
		"--cache-dir", cacheDir,
		"--abi", "arm64-v8a",
		"--abi", "x86_64",
	})
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("relocation failed")) {
		t.Fatalf("command.Run() error = %v, want relocation failure", err)
	}
	if _, err := os.Stat(filepath.Join(armKitDir, ".yaqt", "manifest.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("first kit manifest Stat() error = %v, want no manifest", err)
	}
}

func TestInstallQtRejectsKitStateDirectorySymlinkEscape(t *testing.T) {
	qtRoot := cleanInstallTestRoot(t, "state-directory-symlink")
	kitDir := filepath.Join(qtRoot, "6.11.2", "macos")
	outsideStateDir := filepath.Join(qtRoot, "outside-state")
	if err := os.MkdirAll(kitDir, 0o755); err != nil {
		t.Fatalf("create legacy kit: %v", err)
	}
	if err := os.MkdirAll(outsideStateDir, 0o755); err != nil {
		t.Fatalf("create outside state directory: %v", err)
	}
	if err := os.Symlink(outsideStateDir, filepath.Join(kitDir, ".yaqt")); err != nil {
		t.Skipf("create state directory symlink: %v", err)
	}

	client := &stubRepositoryClient{
		installPlan: qtrepo.InstallPlan{
			Version: qtrepo.Version{Major: 6, Minor: 11, Patch: 2},
			Host:    qtrepo.HostMac,
			Target:  qtrepo.TargetDesktop,
			DesktopKit: &qtrepo.DesktopKit{
				Architecture: qtrepo.DesktopArchitectureMacClang64,
				Destination:  kitDir,
				Packages: []qtrepo.PackageSelection{{
					Name:           "qt.qt6.6112.clang_64",
					PackageVersion: "6.11.2-0-test",
					Archives: []qtrepo.Archive{{
						Name:      "qtbase",
						ExtractTo: kitDir,
					}},
				}},
			},
		},
	}
	command := newCommand(
		commandDependencies{
			install: installCommandDependencies{
				resolver: client,
				relocatorFactory: installRelocatorFactory(func(
					qtrepo.InstallPlan,
					string,
				) (installRelocator, error) {
					return &stubInstallRelocator{}, nil
				}),
			},
		},
		qtrepo.HostMac,
		&bytes.Buffer{},
		&bytes.Buffer{},
	)
	err := command.Run(context.Background(), []string{
		"yaqt", "install-qt", "6.11.2",
		"--target", "desktop",
		"--root", qtRoot,
	})
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("escapes")) {
		t.Fatalf("command.Run() error = %v, want a state directory escape error", err)
	}
	if _, err := os.Stat(filepath.Join(outsideStateDir, "manifest.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("outside manifest Stat() error = %v, want no manifest", err)
	}
}

func TestInstallQtRejectsVersionDirectorySymlinkEscape(t *testing.T) {
	qtRoot := cleanInstallTestRoot(t, "version-directory-symlink")
	outsideRoot := cleanInstallTestRoot(t, "version-directory-symlink-outside")
	if err := os.MkdirAll(qtRoot, 0o755); err != nil {
		t.Fatalf("create Qt root: %v", err)
	}
	if err := os.MkdirAll(outsideRoot, 0o755); err != nil {
		t.Fatalf("create outside root: %v", err)
	}
	if err := os.Symlink(outsideRoot, filepath.Join(qtRoot, "6.11.2")); err != nil {
		t.Skipf("create version directory symlink: %v", err)
	}
	kitDir := filepath.Join(qtRoot, "6.11.2", "macos")
	client := &stubRepositoryClient{
		installPlan: qtrepo.InstallPlan{
			Version: qtrepo.Version{Major: 6, Minor: 11, Patch: 2},
			Host:    qtrepo.HostMac,
			Target:  qtrepo.TargetDesktop,
			DesktopKit: &qtrepo.DesktopKit{
				Architecture: qtrepo.DesktopArchitectureMacClang64,
				Destination:  kitDir,
				Packages: []qtrepo.PackageSelection{{
					Name:           "qt.qt6.6112.clang_64",
					PackageVersion: "6.11.2-0-test",
					Archives: []qtrepo.Archive{{
						Name:      "qtbase",
						ExtractTo: kitDir,
					}},
				}},
			},
		},
	}
	command := newCommand(
		commandDependencies{
			install: installCommandDependencies{
				resolver: client,
				relocatorFactory: installRelocatorFactory(func(
					qtrepo.InstallPlan,
					string,
				) (installRelocator, error) {
					return &stubInstallRelocator{}, nil
				}),
			},
		},
		qtrepo.HostMac,
		&bytes.Buffer{},
		&bytes.Buffer{},
	)

	err := command.Run(context.Background(), []string{
		"yaqt", "install-qt", "6.11.2",
		"--target", "desktop",
		"--root", qtRoot,
	})
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("escapes")) {
		t.Fatalf("command.Run() error = %v, want a state path escape error", err)
	}
	if _, err := os.Stat(filepath.Join(outsideRoot, ".yaqt", "install.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("outside lock Stat() error = %v, want no lock", err)
	}
}

func TestInstallQtRequiresRoot(t *testing.T) {
	client := &stubRepositoryClient{}
	command := newCommand(
		commandDependencies{
			install: installCommandDependencies{resolver: client},
		},
		qtrepo.HostLinux,
		&bytes.Buffer{},
		&bytes.Buffer{},
	)
	err := command.Run(context.Background(), []string{
		"yaqt",
		"install-qt",
		"6.8.0",
		"--target", "android",
		"--abi", "arm64-v8a",
	})
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("root")) {
		t.Fatalf("command.Run() error = %v, want a Qt installation root requirement", err)
	}
}

func TestInstallQtRejectsVersionDirectoryAsRoot(t *testing.T) {
	client := &stubRepositoryClient{}
	command := newCommand(
		commandDependencies{
			install: installCommandDependencies{resolver: client},
		},
		qtrepo.HostLinux,
		&bytes.Buffer{},
		&bytes.Buffer{},
	)
	err := command.Run(context.Background(), []string{
		"yaqt",
		"install-qt",
		"6.8.0",
		"--target", "android",
		"--abi", "arm64-v8a",
		"--root", filepath.Join(".tmp", "Qt", "6.8.0"),
		"--dry-run",
	})
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("includes version 6.8.0")) {
		t.Fatalf("command.Run() error = %v, want a version directory root error", err)
	}
}

func TestInstallQtRejectsRemovedHostQtDirectoryFlag(t *testing.T) {
	client := &stubRepositoryClient{}
	command := newCommand(
		commandDependencies{
			install: installCommandDependencies{resolver: client},
		},
		qtrepo.HostLinux,
		&bytes.Buffer{},
		&bytes.Buffer{},
	)
	err := command.Run(context.Background(), []string{
		"yaqt",
		"install-qt",
		"6.8.0",
		"--target", "android",
		"--abi", "arm64-v8a",
		"--root", filepath.Join(".tmp", "Qt"),
		"--host-qt-dir", filepath.Join(".tmp", "Qt", "6.8.0", "linux_gcc_64"),
		"--dry-run",
	})
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("host-qt-dir")) {
		t.Fatalf("command.Run() error = %v, want an unknown host Qt directory flag error", err)
	}
}

func TestInstallQtCompleteRejectsCrossHostInstallation(t *testing.T) {
	client := &stubRepositoryClient{}
	relocatorFactory := installRelocatorFactory(func(qtrepo.InstallPlan, string) (installRelocator, error) {
		return &stubInstallRelocator{}, nil
	})
	command := newCommand(
		commandDependencies{
			install: installCommandDependencies{
				resolver:         client,
				relocatorFactory: relocatorFactory,
			},
		},
		qtrepo.HostLinux,
		&bytes.Buffer{},
		&bytes.Buffer{},
	)
	err := command.Run(context.Background(), []string{
		"yaqt",
		"install-qt",
		"6.8.0",
		"--host", "windows",
		"--target", "android",
		"--abi", "arm64-v8a",
		"--root", filepath.Join(".tmp", "Qt"),
	})
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("does not match the current host")) {
		t.Fatalf("command.Run() error = %v, want a cross-host installation error", err)
	}
}

func TestInstallQtRejectsConflictingExecutionModes(t *testing.T) {
	for _, modes := range [][]string{
		{"--dry-run", "--download-only"},
		{"--dry-run", "--extract-only"},
		{"--download-only", "--extract-only"},
	} {
		t.Run(modes[0]+"_and_"+modes[1], func(t *testing.T) {
			client := &stubRepositoryClient{}
			command := newCommand(
				commandDependencies{
					install: installCommandDependencies{resolver: client},
				},
				qtrepo.HostLinux,
				&bytes.Buffer{},
				&bytes.Buffer{},
			)
			arguments := []string{
				"yaqt",
				"install-qt",
				"6.8.0",
				"--target", "android",
				"--abi", "arm64-v8a",
				"--root", filepath.Join(".tmp", "Qt"),
			}
			arguments = append(arguments, modes...)
			err := command.Run(context.Background(), arguments)
			if err == nil || !bytes.Contains([]byte(err.Error()), []byte("mutually exclusive")) {
				t.Fatalf("command.Run() error = %v, want a mutually exclusive mode error", err)
			}
		})
	}
}
