package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/zengxs/yaqt/internal/qtrepo"
)

func TestListModulesDesktopCommandUsesNativeArchitecture(t *testing.T) {
	output := &bytes.Buffer{}
	client := &stubRepositoryClient{modules: []string{"qtcharts", "qtmultimedia"}}
	command := newCommand(
		commandDependencies{moduleLister: client},
		qtrepo.HostMac,
		output,
		&bytes.Buffer{},
	)

	err := command.Run(context.Background(), []string{
		"yaqt",
		"list-modules",
		"6.11.2",
		"--base-url", "https://mirror.example/qt",
	})
	if err != nil {
		t.Fatalf("command.Run() error = %v", err)
	}
	if got, want := output.String(), "qtcharts\nqtmultimedia\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
	request := client.moduleRequest
	if got, want := request.Repository.IndexURL(), "https://mirror.example/qt/online/qtsdkrepository/mac_x64/desktop/"; got != want {
		t.Errorf("repository URL = %q, want %q", got, want)
	}
	if got, want := request.Version.String(), "6.11.2"; got != want {
		t.Errorf("version = %q, want %q", got, want)
	}
	if got, want := request.DesktopArchitecture, qtrepo.DesktopArchitectureMacClang64; got != want {
		t.Errorf("desktop architecture = %q, want %q", got, want)
	}
}

func TestListModulesAndroidCommandUsesSelectedABI(t *testing.T) {
	output := &bytes.Buffer{}
	client := &stubRepositoryClient{modules: []string{"qtmultimedia"}}
	command := newCommand(
		commandDependencies{moduleLister: client},
		qtrepo.HostLinux,
		output,
		&bytes.Buffer{},
	)

	err := command.Run(context.Background(), []string{
		"yaqt",
		"list-modules",
		"6.8.0",
		"--target", "android",
		"--abi", "arm64-v8a",
	})
	if err != nil {
		t.Fatalf("command.Run() error = %v", err)
	}
	if got, want := output.String(), "qtmultimedia\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
	request := client.moduleRequest
	if got, want := request.Repository.IndexURL(), qtrepo.DefaultBaseURL+"/online/qtsdkrepository/all_os/android/"; got != want {
		t.Errorf("repository URL = %q, want %q", got, want)
	}
	if got, want := request.AndroidABI, qtrepo.AndroidABIArm64V8A; got != want {
		t.Errorf("Android ABI = %q, want %q", got, want)
	}
}

func TestListModulesIOSCommand(t *testing.T) {
	output := &bytes.Buffer{}
	client := &stubRepositoryClient{modules: []string{"qtmultimedia"}}
	command := newCommand(
		commandDependencies{moduleLister: client},
		qtrepo.HostMac,
		output,
		&bytes.Buffer{},
	)

	err := command.Run(context.Background(), []string{
		"yaqt",
		"list-modules",
		"6.11.2",
		"--target", "ios",
	})
	if err != nil {
		t.Fatalf("command.Run() error = %v", err)
	}
	if got, want := output.String(), "qtmultimedia\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
	request := client.moduleRequest
	if got, want := request.Repository.IndexURL(), qtrepo.DefaultBaseURL+"/online/qtsdkrepository/mac_x64/ios/"; got != want {
		t.Errorf("repository URL = %q, want %q", got, want)
	}
	if request.DesktopArchitecture != "" {
		t.Errorf("desktop architecture = %q, want none", request.DesktopArchitecture)
	}
	if request.AndroidABI != "" {
		t.Errorf("Android ABI = %q, want none", request.AndroidABI)
	}
}

func TestListModulesIOSRejectsArchitectureFlags(t *testing.T) {
	for _, flag := range [][]string{
		{"--arch", "clang_64"},
		{"--abi", "arm64-v8a"},
	} {
		t.Run(flag[0], func(t *testing.T) {
			command := newCommand(
				commandDependencies{moduleLister: &stubRepositoryClient{}},
				qtrepo.HostMac,
				&bytes.Buffer{},
				&bytes.Buffer{},
			)
			args := []string{"yaqt", "list-modules", "6.11.2", "--target", "ios"}
			args = append(args, flag...)
			if err := command.Run(context.Background(), args); err == nil {
				t.Fatalf("command.Run() error = nil, want %s to be rejected", flag[0])
			}
		})
	}
}

func TestListModulesRejectsTargetSpecificArchitectureFlags(t *testing.T) {
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
			want: "--abi is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &stubRepositoryClient{}
			command := newCommand(
				commandDependencies{moduleLister: client},
				qtrepo.HostLinux,
				&bytes.Buffer{},
				&bytes.Buffer{},
			)
			args := []string{"yaqt", "list-modules", "6.8.0"}
			args = append(args, test.args...)
			err := command.Run(context.Background(), args)
			if err == nil || !bytes.Contains([]byte(err.Error()), []byte(test.want)) {
				t.Fatalf("command.Run() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestListModulesRejectsInvalidInvocation(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "missing version",
			args: []string{"yaqt", "list-modules"},
			want: "requires exactly one Qt version",
		},
		{
			name: "unsupported target",
			args: []string{"yaqt", "list-modules", "6.8.0", "--target", "wasm"},
			want: "unsupported Qt package target",
		},
		{
			name: "duplicate Android ABI",
			args: []string{
				"yaqt", "list-modules", "6.8.0",
				"--target", "android",
				"--abi", "arm64-v8a",
				"--abi", "x86_64",
			},
			want: "can't duplicate this flag",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &stubRepositoryClient{}
			command := newCommand(
				commandDependencies{moduleLister: client},
				qtrepo.HostLinux,
				&bytes.Buffer{},
				&bytes.Buffer{},
			)
			err := command.Run(context.Background(), test.args)
			if err == nil || !bytes.Contains([]byte(err.Error()), []byte(test.want)) {
				t.Fatalf("command.Run() error = %v, want %q", err, test.want)
			}
		})
	}
}
