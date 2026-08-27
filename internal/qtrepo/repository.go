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
	HostAllOS        Host = "all_os"
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
	HostAllOS:        "all_os",
}

var targetsByHost = map[Host][]Target{
	HostWindows:      {TargetAndroid, TargetDesktop, TargetWinRT},
	HostWindowsARM64: {TargetDesktop},
	HostMac:          {TargetAndroid, TargetDesktop, TargetIOS},
	HostLinux:        {TargetAndroid, TargetDesktop},
	HostLinuxARM64:   {TargetDesktop},
	HostAllOS:        {TargetAndroid, TargetQt, TargetWASM},
}

// Repository describes one Qt online repository index.
type Repository struct {
	Host   Host
	Target Target

	indexURL string
}

// ParseHost validates a repository host name.
func ParseHost(value string) (Host, error) {
	host := Host(strings.ToLower(strings.TrimSpace(value)))
	if _, ok := hostSegments[host]; !ok {
		return "", fmt.Errorf("unsupported host %q (choose from windows, windows_arm64, mac, linux, linux_arm64, all_os)", value)
	}
	return host, nil
}

// ParseTarget validates a repository target name.
func ParseTarget(value string) (Target, error) {
	target := Target(strings.ToLower(strings.TrimSpace(value)))
	for _, targets := range targetsByHost {
		if slices.Contains(targets, target) {
			return target, nil
		}
	}
	return "", fmt.Errorf("unsupported target %q (choose from desktop, android, winrt, ios, wasm, qt)", value)
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
	segment, ok := hostSegments[host]
	if !ok {
		return Repository{}, fmt.Errorf("unsupported host %q", host)
	}

	allowedTargets := targetsByHost[host]
	if !slices.Contains(allowedTargets, target) {
		values := make([]string, len(allowedTargets))
		for i, allowed := range allowedTargets {
			values[i] = string(allowed)
		}
		return Repository{}, fmt.Errorf(
			"target %q is not available for host %q (choose from %s)",
			target,
			host,
			strings.Join(values, ", "),
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

	parsed.Path = strings.TrimRight(parsed.Path, "/") +
		"/online/qtsdkrepository/" + segment + "/" + string(target) + "/"
	parsed.RawPath = ""

	return Repository{
		Host:     host,
		Target:   target,
		indexURL: parsed.String(),
	}, nil
}

// IndexURL returns the HTML directory index URL for the repository.
func (r Repository) IndexURL() string {
	return r.indexURL
}
