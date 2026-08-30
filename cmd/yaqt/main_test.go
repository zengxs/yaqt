package main

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/zengxs/yaqt/internal/qtrepo"
)

type stubRepositoryClient struct {
	repository     qtrepo.Repository
	versions       []qtrepo.Version
	installRequest qtrepo.InstallRequest
	installPlan    qtrepo.InstallPlan
}

type stubArchiveFetcher struct {
	destination string
	archives    []qtrepo.Archive
}

func (stub *stubArchiveFetcher) Fetch(_ context.Context, archive qtrepo.Archive) (string, error) {
	stub.archives = append(stub.archives, archive)
	return filepath.Join(stub.destination, archive.Name+".7z"), nil
}

func (stub *stubRepositoryClient) ListVersions(_ context.Context, repository qtrepo.Repository) ([]qtrepo.Version, error) {
	stub.repository = repository
	return stub.versions, nil
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
	command := newCommand(lister, lister, nil, qtrepo.HostLinux, output, &bytes.Buffer{})

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
	command := newCommand(client, client, nil, qtrepo.HostLinux, &bytes.Buffer{}, &bytes.Buffer{})
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
			Target:  qtrepo.TargetAndroid,
			HostQt: qtrepo.HostQtRequirement{
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
	command := newCommand(client, client, nil, qtrepo.HostLinux, output, &bytes.Buffer{})

	err := command.Run(context.Background(), []string{
		"yaqt",
		"install-qt",
		"6.8.0",
		"--target", "android",
		"--abi", "arm64-v8a",
		"--module", "qtmultimedia",
		"--output-dir", "/opt/Qt",
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
		"Post-install: relocate Android Qt paths and connect the kit to the matching host Qt.",
	} {
		if !bytes.Contains(output.Bytes(), []byte(want)) {
			t.Errorf("output does not contain %q:\n%s", want, output)
		}
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
	command := newCommand(client, client, factory, qtrepo.HostLinux, output, &bytes.Buffer{})

	err := command.Run(context.Background(), []string{
		"yaqt",
		"install-qt",
		"6.8.0",
		"--target", "android",
		"--abi", "arm64-v8a",
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

func TestInstallQtRequiresExecutionMode(t *testing.T) {
	client := &stubRepositoryClient{}
	command := newCommand(client, client, nil, qtrepo.HostLinux, &bytes.Buffer{}, &bytes.Buffer{})
	err := command.Run(context.Background(), []string{
		"yaqt",
		"install-qt",
		"6.8.0",
		"--target", "android",
		"--abi", "arm64-v8a",
	})
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("--dry-run or --download-only")) {
		t.Fatalf("command.Run() error = %v, want an execution mode requirement", err)
	}
}

func TestInstallQtRejectsConflictingExecutionModes(t *testing.T) {
	client := &stubRepositoryClient{}
	command := newCommand(client, client, nil, qtrepo.HostLinux, &bytes.Buffer{}, &bytes.Buffer{})
	err := command.Run(context.Background(), []string{
		"yaqt",
		"install-qt",
		"6.8.0",
		"--target", "android",
		"--abi", "arm64-v8a",
		"--dry-run",
		"--download-only",
	})
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("mutually exclusive")) {
		t.Fatalf("command.Run() error = %v, want a mutually exclusive mode error", err)
	}
}
