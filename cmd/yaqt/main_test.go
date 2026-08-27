package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/zengxs/yaqt/internal/qtrepo"
)

type stubVersionLister struct {
	repository qtrepo.Repository
	versions   []qtrepo.Version
}

func (stub *stubVersionLister) ListVersions(_ context.Context, repository qtrepo.Repository) ([]qtrepo.Version, error) {
	stub.repository = repository
	return stub.versions, nil
}

func TestListQtCommand(t *testing.T) {
	output := &bytes.Buffer{}
	lister := &stubVersionLister{
		versions: []qtrepo.Version{
			{Major: 6, Minor: 8, Patch: 0},
			{Major: 6, Minor: 8, Patch: 1},
		},
	}
	command := newCommand(lister, qtrepo.HostLinux, output, &bytes.Buffer{})

	err := command.Run(context.Background(), []string{
		"yaqt",
		"list-qt",
		"--host", "all_os",
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
	command := newCommand(&stubVersionLister{}, qtrepo.HostLinux, &bytes.Buffer{}, &bytes.Buffer{})
	err := command.Run(context.Background(), []string{
		"yaqt",
		"list-qt",
		"--host", "linux_arm64",
		"--target", "android",
	})
	if err == nil {
		t.Fatal("command.Run() error = nil, want an invalid host/target error")
	}
}
