package main

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/zengxs/yaqt/internal/qtrepo"
)

type stubRepositoryClient struct {
	repository     qtrepo.Repository
	versions       []qtrepo.Version
	modules        []string
	moduleRequest  qtrepo.ModuleRequest
	installRequest qtrepo.InstallRequest
	installPlan    qtrepo.InstallPlan
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
	return nil
}

func (stub *stubRepositoryClient) ListVersions(_ context.Context, repository qtrepo.Repository) ([]qtrepo.Version, error) {
	stub.repository = repository
	return stub.versions, nil
}

func (stub *stubRepositoryClient) ListModules(
	_ context.Context,
	request qtrepo.ModuleRequest,
) ([]string, error) {
	stub.moduleRequest = request
	return stub.modules, nil
}

func (stub *stubRepositoryClient) ResolveInstall(
	_ context.Context,
	request qtrepo.InstallRequest,
) (qtrepo.InstallPlan, error) {
	stub.installRequest = request
	return stub.installPlan, nil
}

func TestListQtCommand(t *testing.T) {
	output := &bytes.Buffer{}
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
							Name: "qt.qt6.680.android_arm64_v8a",
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
							Name:   "qt.qt6.680.addons.qtmultimedia.android_arm64_v8a",
							Module: "qtmultimedia",
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
						Name: "qt.qt6.6112.clang_64",
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
	qtRoot, err := filepath.Abs(filepath.Join(".tmp", "Qt"))
	if err != nil {
		t.Fatalf("filepath.Abs() error = %v", err)
	}
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
						{Archives: []qtrepo.Archive{archive}},
					},
				},
			},
		},
	}
	cacheDir := filepath.Join(".tmp", "cache")
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

	err = command.Run(context.Background(), []string{
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
	qtRoot, err := filepath.Abs(filepath.Join(".tmp", "desktop-complete-root"))
	if err != nil {
		t.Fatalf("filepath.Abs() error = %v", err)
	}
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
					{Archives: []qtrepo.Archive{archive}},
				},
			},
		},
	}
	cacheDir := filepath.Join(".tmp", "desktop-complete-cache")
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

	err = command.Run(context.Background(), []string{
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
