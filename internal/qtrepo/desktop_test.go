package qtrepo

import (
	"strings"
	"testing"
)

func TestResolveDesktopArchitectureUsesNativeDefaults(t *testing.T) {
	tests := []struct {
		host Host
		want DesktopArchitecture
	}{
		{host: HostMac, want: DesktopArchitectureMacClang64},
		{host: HostLinux, want: DesktopArchitectureLinuxGCC64},
		{host: HostLinuxARM64, want: DesktopArchitectureLinuxGCCARM64},
		{host: HostWindows, want: DesktopArchitectureWindowsMSVC64},
		{host: HostWindowsARM64, want: DesktopArchitectureWindowsMSVCARM64},
	}

	for _, test := range tests {
		t.Run(string(test.host), func(t *testing.T) {
			got, err := ResolveDesktopArchitecture(test.host, "")
			if err != nil {
				t.Fatalf("ResolveDesktopArchitecture() error = %v", err)
			}
			if got != test.want {
				t.Errorf("ResolveDesktopArchitecture() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestResolveDesktopArchitectureRejectsDifferentHost(t *testing.T) {
	_, err := ResolveDesktopArchitecture(HostLinux, string(DesktopArchitectureMacClang64))
	if err == nil || !strings.Contains(err.Error(), "is for host \"mac\"") {
		t.Fatalf("ResolveDesktopArchitecture() error = %v, want a host mismatch", err)
	}
}
