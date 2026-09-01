package qtrepo

import (
	"fmt"
	"strings"
)

// DesktopArchitecture identifies a native desktop Qt package variant.
type DesktopArchitecture string

const (
	DesktopArchitectureMacClang64       DesktopArchitecture = "clang_64"
	DesktopArchitectureLinuxGCC64       DesktopArchitecture = "linux_gcc_64"
	DesktopArchitectureLinuxGCCARM64    DesktopArchitecture = "linux_gcc_arm64"
	DesktopArchitectureWindowsMSVC64    DesktopArchitecture = "win64_msvc2022_64"
	DesktopArchitectureWindowsMSVCARM64 DesktopArchitecture = "win64_msvc2022_arm64"
)

type desktopArchitectureDescriptor struct {
	host                            Host
	installDirectory                string
	repositorySuffix                string
	extensionRepositoryArchitecture string
}

// desktopArchitectureDescriptors defines the native variants that yaqt can
// currently install. Repository architecture discovery is metadata-driven and
// may report additional variants.
var desktopArchitectureDescriptors = map[DesktopArchitecture]desktopArchitectureDescriptor{
	DesktopArchitectureMacClang64: {
		host:                            HostMac,
		installDirectory:                "macos",
		extensionRepositoryArchitecture: "clang_64",
	},
	DesktopArchitectureLinuxGCC64: {
		host:                            HostLinux,
		installDirectory:                "gcc_64",
		extensionRepositoryArchitecture: "x86_64",
	},
	DesktopArchitectureLinuxGCCARM64: {
		host:                            HostLinuxARM64,
		installDirectory:                "gcc_arm64",
		extensionRepositoryArchitecture: "arm64",
	},
	DesktopArchitectureWindowsMSVC64: {
		host:                            HostWindows,
		installDirectory:                "msvc2022_64",
		repositorySuffix:                "msvc2022_64",
		extensionRepositoryArchitecture: "msvc2022_64",
	},
	DesktopArchitectureWindowsMSVCARM64: {
		host:                            HostWindowsARM64,
		installDirectory:                "msvc2022_arm64",
		extensionRepositoryArchitecture: "msvc2022_arm64",
	},
}

var defaultDesktopArchitectures = map[Host]DesktopArchitecture{
	HostMac:          DesktopArchitectureMacClang64,
	HostLinux:        DesktopArchitectureLinuxGCC64,
	HostLinuxARM64:   DesktopArchitectureLinuxGCCARM64,
	HostWindows:      DesktopArchitectureWindowsMSVC64,
	HostWindowsARM64: DesktopArchitectureWindowsMSVCARM64,
}

// ResolveDesktopArchitecture validates an explicit architecture or returns the
// native default for host when value is empty.
func ResolveDesktopArchitecture(host Host, value string) (DesktopArchitecture, error) {
	if strings.TrimSpace(value) == "" {
		architecture, ok := defaultDesktopArchitectures[host]
		if !ok {
			return "", fmt.Errorf("no default desktop Qt architecture for host %q", host)
		}
		return architecture, nil
	}

	architecture := DesktopArchitecture(strings.ToLower(strings.TrimSpace(value)))
	descriptor, ok := desktopArchitectureDescriptors[architecture]
	if !ok {
		return "", fmt.Errorf("unsupported desktop Qt architecture %q", value)
	}
	if descriptor.host != host {
		return "", fmt.Errorf(
			"desktop Qt architecture %q is for host %q, not %q",
			architecture,
			descriptor.host,
			host,
		)
	}
	return architecture, nil
}

func (architecture DesktopArchitecture) descriptor() (desktopArchitectureDescriptor, error) {
	descriptor, ok := desktopArchitectureDescriptors[architecture]
	if !ok {
		return desktopArchitectureDescriptor{}, fmt.Errorf(
			"unsupported desktop Qt architecture %q",
			architecture,
		)
	}
	return descriptor, nil
}
