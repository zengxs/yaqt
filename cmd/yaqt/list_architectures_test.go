package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/zengxs/yaqt/internal/qtrepo"
)

func TestListArchitecturesCommand(t *testing.T) {
	output := &bytes.Buffer{}
	client := &stubRepositoryClient{
		architectures: []string{"win64_mingw", "win64_msvc2022_64"},
	}
	command := newCommand(
		commandDependencies{architectureLister: client},
		qtrepo.HostMac,
		output,
		&bytes.Buffer{},
	)

	err := command.Run(context.Background(), []string{
		"yaqt",
		"list-architectures",
		"6.11.2",
		"--host", "windows",
		"--target", "desktop",
		"--base-url", "https://mirror.example/qt",
	})
	if err != nil {
		t.Fatalf("command.Run() error = %v", err)
	}
	if got, want := output.String(), "win64_mingw\nwin64_msvc2022_64\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
	request := client.architectureRequest
	if got, want := request.Repository.IndexURL(), "https://mirror.example/qt/online/qtsdkrepository/windows_x86/desktop/"; got != want {
		t.Errorf("repository URL = %q, want %q", got, want)
	}
	if got, want := request.Version.String(), "6.11.2"; got != want {
		t.Errorf("version = %q, want %q", got, want)
	}
}

func TestListArchitecturesRejectsInvalidInvocation(t *testing.T) {
	for _, args := range [][]string{
		{"yaqt", "list-architectures"},
		{"yaqt", "list-architectures", "6.11.2", "extra"},
	} {
		command := newCommand(
			commandDependencies{architectureLister: &stubRepositoryClient{}},
			qtrepo.HostLinux,
			&bytes.Buffer{},
			&bytes.Buffer{},
		)
		if err := command.Run(context.Background(), args); err == nil {
			t.Fatalf("command.Run(%v) error = nil, want an argument error", args)
		}
	}
}
