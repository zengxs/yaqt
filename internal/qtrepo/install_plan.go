package qtrepo

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strings"
)

// InstallRequest describes one Qt installation to resolve from repository metadata.
type InstallRequest struct {
	Repository          Repository
	Version             Version
	DesktopArchitecture DesktopArchitecture
	AndroidABIs         []AndroidABI
	Modules             []string
	Destination         string
}

// InstallPlan contains the artifacts and layout required for an installation.
type InstallPlan struct {
	Version     Version
	Host        Host
	Target      Target
	HostQt      *QtInstallationIdentity
	DesktopKit  *DesktopKit
	AndroidKits []AndroidKit
}

// QtInstallationIdentity identifies a Qt installation by host and version.
type QtInstallationIdentity struct {
	Host    Host
	Version Version
}

// DesktopKit contains the artifacts for one native desktop Qt installation.
type DesktopKit struct {
	Architecture DesktopArchitecture
	Destination  string
	Packages     []PackageSelection
}

// AndroidKit contains the artifacts for one Android ABI.
type AndroidKit struct {
	ABI         AndroidABI
	Destination string
	Packages    []PackageSelection
}

// PackageSelection records why a repository package was selected and its artifacts.
// Module is empty for the base package.
type PackageSelection struct {
	Name     string
	Module   string
	Archives []Archive
}

// ChecksumAlgorithm identifies a supported archive checksum algorithm.
type ChecksumAlgorithm string

const (
	// ChecksumSHA256 selects SHA-256 sidecars and verification.
	ChecksumSHA256 ChecksumAlgorithm = "sha256"
)

// Checksum describes how to verify one downloadable archive.
type Checksum struct {
	Algorithm ChecksumAlgorithm
	URL       string
}

// Archive describes one downloadable Qt archive and where to extract it.
type Archive struct {
	Name      string
	URL       string
	Checksum  Checksum
	ExtractTo string
}

type selectedPackage struct {
	update packageUpdate
	module string
}

var legacyArchiveLayoutVersion = Version{Major: 6, Minor: 8}

// ResolveInstall converts repository metadata into a deterministic installation plan.
func (c *Client) ResolveInstall(ctx context.Context, request InstallRequest) (InstallPlan, error) {
	if err := validateSupportedVersion(request.Version); err != nil {
		return InstallPlan{}, err
	}
	if strings.TrimSpace(request.Destination) == "" {
		return InstallPlan{}, fmt.Errorf("installation destination must not be empty")
	}

	modules, err := normalizeModules(request.Modules)
	if err != nil {
		return InstallPlan{}, err
	}
	destination := filepath.Clean(request.Destination)
	operations, ok := packageTargets[request.Repository.Target]
	if !ok {
		return InstallPlan{}, fmt.Errorf("installation planning supports only the desktop and Android targets")
	}
	return operations.planInstall(c, ctx, request, modules, destination)
}

func (c *Client) resolveDesktopInstall(
	ctx context.Context,
	request InstallRequest,
	modules []string,
	destination string,
) (InstallPlan, error) {
	if len(request.AndroidABIs) != 0 {
		return InstallPlan{}, fmt.Errorf("Android ABIs cannot be used with the desktop target")
	}
	architecture, err := ResolveDesktopArchitecture(
		request.Repository.Host,
		string(request.DesktopArchitecture),
	)
	if err != nil {
		return InstallPlan{}, err
	}
	kit, err := c.resolveDesktopKit(
		ctx,
		request.Repository,
		request.Version,
		architecture,
		modules,
		destination,
	)
	if err != nil {
		return InstallPlan{}, err
	}
	return InstallPlan{
		Version:    request.Version,
		Host:       request.Repository.Host,
		Target:     TargetDesktop,
		DesktopKit: &kit,
	}, nil
}

func (c *Client) resolveAndroidInstall(
	ctx context.Context,
	request InstallRequest,
	modules []string,
	destination string,
) (InstallPlan, error) {
	if request.DesktopArchitecture != "" {
		return InstallPlan{}, fmt.Errorf("desktop architecture cannot be used with the Android target")
	}
	abis, err := normalizeAndroidABIs(request.AndroidABIs)
	if err != nil {
		return InstallPlan{}, err
	}
	plan := InstallPlan{
		Version: request.Version,
		Host:    request.Repository.Host,
		Target:  TargetAndroid,
		HostQt: &QtInstallationIdentity{
			Host:    request.Repository.Host,
			Version: request.Version,
		},
		AndroidKits: make([]AndroidKit, 0, len(abis)),
	}
	for _, abi := range abis {
		kit, err := c.resolveAndroidKit(
			ctx,
			request.Repository,
			request.Version,
			abi,
			modules,
			destination,
		)
		if err != nil {
			return InstallPlan{}, err
		}
		plan.AndroidKits = append(plan.AndroidKits, kit)
	}
	return plan, nil
}

func (c *Client) resolveDesktopKit(
	ctx context.Context,
	repository Repository,
	version Version,
	architecture DesktopArchitecture,
	modules []string,
	destination string,
) (DesktopKit, error) {
	metadata, descriptor, err := desktopPackageVariantMetadata(repository, version, architecture)
	if err != nil {
		return DesktopKit{}, err
	}
	packages, err := c.fetchPackageUpdates(ctx, metadata.url)
	if err != nil {
		return DesktopKit{}, err
	}

	selected, err := selectPackageUpdates(
		packages,
		metadata,
		modules,
		version,
	)
	if err != nil {
		return DesktopKit{}, err
	}
	packageSelections, err := resolvePackageSelections(
		metadata,
		destination,
		version,
		selected,
	)
	if err != nil {
		return DesktopKit{}, err
	}

	return DesktopKit{
		Architecture: architecture,
		Destination: filepath.Join(
			destination,
			version.String(),
			descriptor.installDirectory,
		),
		Packages: packageSelections,
	}, nil
}

func (c *Client) resolveAndroidKit(
	ctx context.Context,
	repository Repository,
	version Version,
	abi AndroidABI,
	modules []string,
	destination string,
) (AndroidKit, error) {
	metadata, err := androidPackageVariantMetadata(repository, version, abi)
	if err != nil {
		return AndroidKit{}, err
	}
	packages, err := c.fetchPackageUpdates(ctx, metadata.url)
	if err != nil {
		return AndroidKit{}, err
	}

	selected, err := selectPackageUpdates(
		packages,
		metadata,
		modules,
		version,
	)
	if err != nil {
		return AndroidKit{}, err
	}
	packageSelections, err := resolvePackageSelections(metadata, destination, version, selected)
	if err != nil {
		return AndroidKit{}, err
	}

	return AndroidKit{
		ABI:         abi,
		Destination: filepath.Join(destination, version.String(), metadata.packageArchitecture),
		Packages:    packageSelections,
	}, nil
}

func selectPackageUpdates(
	packages map[string]packageUpdate,
	metadata packageVariantMetadata,
	modules []string,
	version Version,
) ([]selectedPackage, error) {
	selected := make([]selectedPackage, 0, len(modules)+1)
	basePackage, ok := packages[metadata.basePackageName()]
	if !ok {
		return nil, fmt.Errorf("Qt %s metadata contains no base package for %s", version, metadata.description)
	}
	selected = append(selected, selectedPackage{update: basePackage})

	for _, module := range modules {
		modulePackage, found := selectModulePackageUpdate(packages, metadata, module)
		if !found {
			return nil, fmt.Errorf(
				"Qt %s module %q is not available for %s",
				version,
				module,
				metadata.description,
			)
		}
		selected = append(selected, selectedPackage{update: modulePackage, module: module})
	}
	return selected, nil
}

func selectModulePackageUpdate(
	packages map[string]packageUpdate,
	metadata packageVariantMetadata,
	module string,
) (packageUpdate, bool) {
	for _, candidate := range metadata.modulePackageNames(module) {
		if update, ok := packages[candidate]; ok {
			return update, true
		}
	}
	return packageUpdate{}, false
}

func resolvePackageSelections(
	metadata packageVariantMetadata,
	destination string,
	version Version,
	selected []selectedPackage,
) ([]PackageSelection, error) {
	packageSelections := make([]PackageSelection, 0, len(selected))
	for _, selection := range selected {
		resolved, err := resolvePackageArchives(metadata.url, destination, version, selection.update)
		if err != nil {
			return nil, fmt.Errorf("resolve package %q: %w", selection.update.Name, err)
		}
		if len(resolved) == 0 {
			return nil, fmt.Errorf("Qt package %q contains no downloadable archives", selection.update.Name)
		}
		packageSelections = append(packageSelections, PackageSelection{
			Name:     strings.TrimSpace(selection.update.Name),
			Module:   selection.module,
			Archives: resolved,
		})
	}
	return packageSelections, nil
}

func resolvePackageArchives(
	metadataURL string,
	destination string,
	version Version,
	update packageUpdate,
) ([]Archive, error) {
	packageName := strings.TrimSpace(update.Name)
	fullVersion := strings.TrimSpace(update.Version)
	if !safeURLSegment(packageName) || !safeURLSegment(fullVersion) {
		return nil, fmt.Errorf("metadata contains an unsafe package name or version")
	}

	extractPaths, err := extractPathsByArchive(update.Operations)
	if err != nil {
		return nil, err
	}
	archiveNames := splitMetadataList(update.DownloadableArchives)
	if len(archiveNames) > 0 && len(extractPaths) == 0 && version != legacyArchiveLayoutVersion {
		return nil, fmt.Errorf("Qt %s package metadata contains no Extract operations", version)
	}
	archives := make([]Archive, 0, len(archiveNames))
	for _, archiveName := range archiveNames {
		if !safeURLSegment(archiveName) || !strings.HasSuffix(strings.ToLower(archiveName), ".7z") {
			return nil, fmt.Errorf("metadata contains an unsafe archive name %q", archiveName)
		}
		archiveURL, err := url.JoinPath(
			strings.TrimSuffix(metadataURL, "Updates.xml"),
			packageName,
			fullVersion+archiveName,
		)
		if err != nil {
			return nil, fmt.Errorf("construct archive URL: %w", err)
		}

		relativePath, hasExtractPath := extractPaths[archiveName]
		if len(extractPaths) > 0 && !hasExtractPath {
			return nil, fmt.Errorf("metadata contains no Extract operation for archive %q", archiveName)
		}
		extractTo := destination
		if relativePath != "" {
			extractTo = filepath.Join(destination, filepath.FromSlash(relativePath))
		}
		logicalName, _, _ := strings.Cut(archiveName, "-")
		logicalName = strings.TrimSuffix(logicalName, ".7z")
		archives = append(archives, Archive{
			Name: logicalName,
			URL:  archiveURL,
			Checksum: Checksum{
				Algorithm: ChecksumSHA256,
				URL:       archiveURL + ".sha256",
			},
			ExtractTo: extractTo,
		})
	}
	return archives, nil
}

func extractPathsByArchive(operations []updateOperation) (map[string]string, error) {
	result := make(map[string]string)
	for _, operation := range operations {
		if operation.Name != "Extract" {
			continue
		}
		if len(operation.Arguments) != 2 {
			return nil, fmt.Errorf("Extract operation must contain a destination and archive name")
		}
		archiveName := strings.TrimSpace(operation.Arguments[1])
		if !safeURLSegment(archiveName) {
			return nil, fmt.Errorf("Extract operation contains an unsafe archive name %q", archiveName)
		}
		relativePath, err := targetRelativePath(operation.Arguments[0])
		if err != nil {
			return nil, err
		}
		result[archiveName] = relativePath
	}
	return result, nil
}

func targetRelativePath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "\\") {
		return "", fmt.Errorf("Extract destination %q must use forward slashes", value)
	}
	if value == "@TargetDir@" {
		return "", nil
	}
	const prefix = "@TargetDir@/"
	if !strings.HasPrefix(value, prefix) {
		return "", fmt.Errorf("Extract destination %q is not relative to @TargetDir@", value)
	}
	relativePath := path.Clean(strings.TrimPrefix(value, prefix))
	if relativePath == "." {
		return "", nil
	}
	nativePath := filepath.FromSlash(relativePath)
	if path.IsAbs(relativePath) || filepath.IsAbs(nativePath) || filepath.VolumeName(nativePath) != "" ||
		relativePath == ".." || strings.HasPrefix(relativePath, "../") {
		return "", fmt.Errorf("Extract destination %q escapes @TargetDir@", value)
	}
	return relativePath, nil
}

func splitMetadataList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if item := strings.TrimSpace(part); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func normalizeAndroidABIs(values []AndroidABI) ([]AndroidABI, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("at least one Android ABI is required")
	}
	seen := make(map[AndroidABI]struct{}, len(values))
	result := make([]AndroidABI, 0, len(values))
	for _, value := range values {
		parsed, err := ParseAndroidABI(string(value))
		if err != nil {
			return nil, err
		}
		if _, ok := seen[parsed]; ok {
			continue
		}
		seen[parsed] = struct{}{}
		result = append(result, parsed)
	}
	return result, nil
}

func normalizeModules(values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		module := strings.ToLower(strings.TrimSpace(value))
		if !validModuleName(module) {
			return nil, fmt.Errorf("invalid Qt module name %q", value)
		}
		if _, ok := seen[module]; ok {
			continue
		}
		seen[module] = struct{}{}
		result = append(result, module)
	}
	return result, nil
}

func validModuleName(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') &&
			(character < '0' || character > '9') && character != '_' {
			return false
		}
	}
	return true
}

func safeURLSegment(value string) bool {
	return value != "" && value != "." && value != ".." &&
		path.Base(value) == value && !strings.Contains(value, "\\")
}
