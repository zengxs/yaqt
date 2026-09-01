package qtrepo

import (
	"fmt"
	"net/url"
	"runtime"
	"slices"
	"strings"
)

// DefaultBaseURL is the root of the official Qt download server.
const DefaultBaseURL = "https://download.qt.io"

// Host identifies the machine that runs the Qt SDK.
type Host string

const (
	HostWindows      Host = "windows"
	HostWindowsARM64 Host = "windows_arm64"
	HostMac          Host = "mac"
	HostLinux        Host = "linux"
	HostLinuxARM64   Host = "linux_arm64"
)

// Target identifies the platform targeted by the Qt SDK.
type Target string

const (
	TargetDesktop Target = "desktop"
	TargetAndroid Target = "android"
	TargetWinRT   Target = "winrt"
	TargetIOS     Target = "ios"
	TargetWASM    Target = "wasm"
	TargetQt      Target = "qt"
)

var hostSegments = map[Host]string{
	HostWindows:      "windows_x86",
	HostWindowsARM64: "windows_arm64",
	HostMac:          "mac_x64",
	HostLinux:        "linux_x64",
	HostLinuxARM64:   "linux_arm64",
}

type targetDescriptor struct {
	target            Target
	repositorySegment string
	hosts             []Host
}

// targetDescriptors centralizes host eligibility and internal repository routing.
// Android, WebAssembly, and shared Qt content use Qt's platform-independent all_os segment.
var targetDescriptors = []targetDescriptor{
	{target: TargetDesktop},
	{target: TargetAndroid, repositorySegment: "all_os"},
	{target: TargetWinRT, hosts: []Host{HostWindows}},
	{target: TargetIOS, hosts: []Host{HostMac}},
	{target: TargetWASM, repositorySegment: "all_os"},
	{target: TargetQt, repositorySegment: "all_os"},
}

func (descriptor targetDescriptor) allowsHost(host Host) bool {
	return len(descriptor.hosts) == 0 || slices.Contains(descriptor.hosts, host)
}

func descriptorForTarget(target Target) (targetDescriptor, bool) {
	for _, descriptor := range targetDescriptors {
		if descriptor.target == target {
			return descriptor, true
		}
	}
	return targetDescriptor{}, false
}

func targetNamesForHost(host Host) []string {
	names := make([]string, 0, len(targetDescriptors))
	for _, descriptor := range targetDescriptors {
		if descriptor.allowsHost(host) {
			names = append(names, string(descriptor.target))
		}
	}
	return names
}

func targetNames() []string {
	names := make([]string, len(targetDescriptors))
	for index, descriptor := range targetDescriptors {
		names[index] = string(descriptor.target)
	}
	return names
}

// Repository describes one Qt online repository index.
type Repository struct {
	Host   Host
	Target Target

	indexURL          string
	repositoryRootURL string
}

// ParseHost validates a repository host name.
func ParseHost(value string) (Host, error) {
	host := Host(strings.ToLower(strings.TrimSpace(value)))
	if _, ok := hostSegments[host]; !ok {
		return "", fmt.Errorf("unsupported host %q (choose from windows, windows_arm64, mac, linux, linux_arm64)", value)
	}
	return host, nil
}

// ParseTarget validates a repository target name.
func ParseTarget(value string) (Target, error) {
	target := Target(strings.ToLower(strings.TrimSpace(value)))
	if _, ok := descriptorForTarget(target); ok {
		return target, nil
	}
	return "", fmt.Errorf("unsupported target %q (choose from %s)", value, strings.Join(targetNames(), ", "))
}

// CurrentHost returns the repository host for the running platform.
func CurrentHost() (Host, error) {
	return HostForPlatform(runtime.GOOS, runtime.GOARCH)
}

// HostForPlatform maps a Go platform to a Qt repository host.
func HostForPlatform(goos, goarch string) (Host, error) {
	switch goos {
	case "windows":
		switch goarch {
		case "amd64":
			return HostWindows, nil
		case "arm64":
			return HostWindowsARM64, nil
		}
	case "darwin":
		if goarch == "amd64" || goarch == "arm64" {
			return HostMac, nil
		}
	case "linux":
		switch goarch {
		case "amd64":
			return HostLinux, nil
		case "arm64":
			return HostLinuxARM64, nil
		}
	}
	return "", fmt.Errorf("cannot infer a Qt repository host for %s/%s", goos, goarch)
}

// NewRepository validates a repository selection and constructs its index URL.
func NewRepository(baseURL string, host Host, target Target) (Repository, error) {
	hostSegment, ok := hostSegments[host]
	if !ok {
		return Repository{}, fmt.Errorf("unsupported host %q", host)
	}

	descriptor, ok := descriptorForTarget(target)
	if !ok || !descriptor.allowsHost(host) {
		return Repository{}, fmt.Errorf(
			"target %q is not available for host %q (choose from %s)",
			target,
			host,
			strings.Join(targetNamesForHost(host), ", "),
		)
	}

	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return Repository{}, fmt.Errorf("parse repository base URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return Repository{}, fmt.Errorf("repository base URL must use http or https")
	}
	if parsed.Host == "" {
		return Repository{}, fmt.Errorf("repository base URL must include a host")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return Repository{}, fmt.Errorf("repository base URL must not include a query or fragment")
	}

	segment := hostSegment
	if descriptor.repositorySegment != "" {
		segment = descriptor.repositorySegment
	}

	parsed.Path = strings.TrimRight(parsed.Path, "/") +
		"/online/qtsdkrepository/" + segment + "/"
	parsed.RawPath = ""
	repositoryRootURL := parsed.String()
	parsed.Path += string(target) + "/"

	return Repository{
		Host:              host,
		Target:            target,
		indexURL:          parsed.String(),
		repositoryRootURL: repositoryRootURL,
	}, nil
}

// IndexURL returns the HTML directory index URL for the repository.
func (r Repository) IndexURL() string {
	return r.indexURL
}
