package qtrepo

import (
	"fmt"
	"strings"
)

// AndroidABI identifies an Android application binary interface.
type AndroidABI string

const (
	AndroidABIArm64V8A   AndroidABI = "arm64-v8a"
	AndroidABIArmeabiV7A AndroidABI = "armeabi-v7a"
	AndroidABIX86        AndroidABI = "x86"
	AndroidABIX8664      AndroidABI = "x86_64"
)

type androidABIDescriptor struct {
	abi            AndroidABI
	repositoryName string
}

var androidABIDescriptors = []androidABIDescriptor{
	{abi: AndroidABIArm64V8A, repositoryName: "arm64_v8a"},
	{abi: AndroidABIArmeabiV7A, repositoryName: "armv7"},
	{abi: AndroidABIX86, repositoryName: "x86"},
	{abi: AndroidABIX8664, repositoryName: "x86_64"},
}

func descriptorForAndroidABI(abi AndroidABI) (androidABIDescriptor, bool) {
	for _, descriptor := range androidABIDescriptors {
		if descriptor.abi == abi {
			return descriptor, true
		}
	}
	return androidABIDescriptor{}, false
}

func androidABINames() []string {
	names := make([]string, len(androidABIDescriptors))
	for index, descriptor := range androidABIDescriptors {
		names[index] = string(descriptor.abi)
	}
	return names
}

// ParseAndroidABI validates an Android ABI using its standard Android spelling.
func ParseAndroidABI(value string) (AndroidABI, error) {
	abi := AndroidABI(strings.ToLower(strings.TrimSpace(value)))
	if _, ok := descriptorForAndroidABI(abi); ok {
		return abi, nil
	}
	return "", fmt.Errorf(
		"unsupported Android ABI %q (choose from %s)",
		value,
		strings.Join(androidABINames(), ", "),
	)
}

func (abi AndroidABI) repositoryName() string {
	descriptor, _ := descriptorForAndroidABI(abi)
	return descriptor.repositoryName
}

func (abi AndroidABI) packageArchitecture() string {
	repositoryName := abi.repositoryName()
	if repositoryName == "" {
		return ""
	}
	return "android_" + repositoryName
}
