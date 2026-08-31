package qtrepo

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

const moduleValidationDestination = "."

// ModuleRequest selects the Qt package variant whose additional modules
// should be listed.
type ModuleRequest struct {
	Repository          Repository
	Version             Version
	DesktopArchitecture DesktopArchitecture
	AndroidABI          AndroidABI
}

// ListModules returns additional installable Qt modules for one package
// variant, sorted by module name.
func (c *Client) ListModules(
	ctx context.Context,
	request ModuleRequest,
) ([]string, error) {
	if err := validateSupportedVersion(request.Version); err != nil {
		return nil, err
	}

	operations, ok := packageTargets[request.Repository.Target]
	if !ok {
		return nil, fmt.Errorf(
			"module listing does not support target %q",
			request.Repository.Target,
		)
	}
	metadata, err := operations.resolveModuleVariant(request)
	if err != nil {
		return nil, err
	}
	packages, err := c.fetchPackageUpdates(ctx, metadata.url)
	if err != nil {
		return nil, err
	}

	baseSelection, err := selectPackageUpdates(packages, metadata, nil, request.Version)
	if err != nil {
		return nil, err
	}
	if _, err := resolvePackageSelections(
		metadata,
		moduleValidationDestination,
		request.Version,
		baseSelection,
	); err != nil {
		return nil, err
	}
	return availableModuleNames(packages, metadata, request.Version), nil
}

func desktopModuleVariant(request ModuleRequest) (packageVariantMetadata, error) {
	if request.AndroidABI != "" {
		return packageVariantMetadata{}, fmt.Errorf("Android ABI cannot be used with the desktop target")
	}
	architecture, err := ResolveDesktopArchitecture(
		request.Repository.Host,
		string(request.DesktopArchitecture),
	)
	if err != nil {
		return packageVariantMetadata{}, err
	}
	metadata, _, err := desktopPackageVariantMetadata(
		request.Repository,
		request.Version,
		architecture,
	)
	if err != nil {
		return packageVariantMetadata{}, err
	}
	return metadata, nil
}

func androidModuleVariant(request ModuleRequest) (packageVariantMetadata, error) {
	if request.DesktopArchitecture != "" {
		return packageVariantMetadata{}, fmt.Errorf("desktop architecture cannot be used with the Android target")
	}
	if request.AndroidABI == "" {
		return packageVariantMetadata{}, fmt.Errorf("Android ABI is required for module listing")
	}
	abi, err := ParseAndroidABI(string(request.AndroidABI))
	if err != nil {
		return packageVariantMetadata{}, err
	}
	metadata, err := androidPackageVariantMetadata(request.Repository, request.Version, abi)
	if err != nil {
		return packageVariantMetadata{}, err
	}
	return metadata, nil
}

func availableModuleNames(
	packages map[string]packageUpdate,
	metadata packageVariantMetadata,
	version Version,
) []string {
	candidates := make(map[string]struct{})
	suffix := "." + metadata.packageArchitecture
	for packageName := range packages {
		name, ok := strings.CutPrefix(packageName, metadata.packagePrefix)
		if !ok {
			continue
		}
		name, ok = strings.CutSuffix(name, suffix)
		if !ok {
			continue
		}
		name = strings.TrimPrefix(name, "addons.")
		if !validModuleName(name) {
			continue
		}
		packageNames := metadata.modulePackageNames(name)
		if packageName != packageNames[0] && packageName != packageNames[1] {
			continue
		}
		candidates[name] = struct{}{}
	}

	names := make([]string, 0, len(candidates))
	for module := range candidates {
		names = append(names, module)
	}
	slices.Sort(names)

	result := make([]string, 0, len(names))
	for _, module := range names {
		update, ok := selectModulePackageUpdate(packages, metadata, module)
		if !ok {
			continue
		}
		selection := []selectedPackage{{update: update, module: module}}
		if _, err := resolvePackageSelections(
			metadata,
			moduleValidationDestination,
			version,
			selection,
		); err != nil {
			continue
		}
		result = append(result, module)
	}
	return result
}
