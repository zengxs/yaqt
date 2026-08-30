package qtrepo

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strings"
)

// InstallRequest describes one Qt installation to resolve from repository metadata.
type InstallRequest struct {
	Repository  Repository
	Version     Version
	AndroidABIs []AndroidABI
	Modules     []string
	Destination string
}

// InstallPlan contains the artifacts and layout required for an installation.
type InstallPlan struct {
	Version     Version
	Target      Target
	HostQt      HostQtRequirement
	AndroidKits []AndroidKit
}

// HostQtRequirement identifies the desktop Qt installation needed by a target SDK.
type HostQtRequirement struct {
	Host    Host
	Version Version
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

// Archive describes one downloadable Qt archive and where to extract it.
type Archive struct {
	Name        string
	URL         string
	ChecksumURL string
	ExtractTo   string
}

type updatesDocument struct {
	Packages []packageUpdate `xml:"PackageUpdate"`
}

type packageUpdate struct {
	Name                 string            `xml:"Name"`
	Version              string            `xml:"Version"`
	DownloadableArchives string            `xml:"DownloadableArchives"`
	Operations           []updateOperation `xml:"Operations>Operation"`
}

type updateOperation struct {
	Name      string   `xml:"name,attr"`
	Arguments []string `xml:"Argument"`
}

type selectedPackage struct {
	update packageUpdate
	module string
}

var legacyAndroidArchiveLayoutVersion = Version{Major: 6, Minor: 8}

// ResolveInstall converts repository metadata into a deterministic installation plan.
func (c *Client) ResolveInstall(ctx context.Context, request InstallRequest) (InstallPlan, error) {
	if request.Repository.Target != TargetAndroid {
		return InstallPlan{}, fmt.Errorf("installation planning currently supports only the Android target")
	}
	if request.Version.compare(minimumSupportedVersion) < 0 {
		return InstallPlan{}, fmt.Errorf("minimum supported version is %s", minimumSupportedVersion)
	}
	if request.Version.Major <= 0 || request.Version.Minor < 0 || request.Version.Patch < 0 {
		return InstallPlan{}, fmt.Errorf("invalid Qt version %s", request.Version)
	}
	if strings.TrimSpace(request.Destination) == "" {
		return InstallPlan{}, fmt.Errorf("installation destination must not be empty")
	}

	abis, err := normalizeAndroidABIs(request.AndroidABIs)
	if err != nil {
		return InstallPlan{}, err
	}
	modules, err := normalizeModules(request.Modules)
	if err != nil {
		return InstallPlan{}, err
	}

	plan := InstallPlan{
		Version: request.Version,
		Target:  TargetAndroid,
		HostQt: HostQtRequirement{
			Host:    request.Repository.Host,
			Version: request.Version,
		},
		AndroidKits: make([]AndroidKit, 0, len(abis)),
	}
	destination := filepath.Clean(request.Destination)
	for _, abi := range abis {
		kit, err := c.resolveAndroidKit(ctx, request.Repository, request.Version, abi, modules, destination)
		if err != nil {
			return InstallPlan{}, err
		}
		plan.AndroidKits = append(plan.AndroidKits, kit)
	}
	return plan, nil
}

func (c *Client) resolveAndroidKit(
	ctx context.Context,
	repository Repository,
	version Version,
	abi AndroidABI,
	modules []string,
	destination string,
) (AndroidKit, error) {
	metadataURL, err := androidMetadataURL(repository, version, abi)
	if err != nil {
		return AndroidKit{}, err
	}
	metadata, err := c.fetchResource(ctx, metadataURL, packageMetadataResource)
	if err != nil {
		return AndroidKit{}, err
	}

	var updates updatesDocument
	if err := xml.Unmarshal(metadata, &updates); err != nil {
		return AndroidKit{}, fmt.Errorf("parse Qt package metadata %s: %w", metadataURL, err)
	}
	packages := make(map[string]packageUpdate, len(updates.Packages))
	for _, update := range updates.Packages {
		packages[strings.TrimSpace(update.Name)] = update
	}

	packagePrefix := fmt.Sprintf("qt.qt%d.%s.", version.Major, version.compact())
	packageArchitecture := abi.packageArchitecture()
	baseName := packagePrefix + packageArchitecture
	selected := make([]selectedPackage, 0, len(modules)+1)
	basePackage, ok := packages[baseName]
	if !ok {
		return AndroidKit{}, fmt.Errorf("Qt %s metadata contains no base package for Android ABI %s", version, abi)
	}
	selected = append(selected, selectedPackage{update: basePackage})

	for _, module := range modules {
		candidates := []string{
			packagePrefix + "addons." + module + "." + packageArchitecture,
			packagePrefix + module + "." + packageArchitecture,
		}
		var modulePackage packageUpdate
		found := false
		for _, candidate := range candidates {
			if update, ok := packages[candidate]; ok {
				modulePackage = update
				found = true
				break
			}
		}
		if !found {
			return AndroidKit{}, fmt.Errorf("Qt %s module %q is not available for Android ABI %s", version, module, abi)
		}
		selected = append(selected, selectedPackage{update: modulePackage, module: module})
	}

	packageSelections := make([]PackageSelection, 0, len(selected))
	for _, selection := range selected {
		resolved, err := resolvePackageArchives(metadataURL, destination, version, selection.update)
		if err != nil {
			return AndroidKit{}, fmt.Errorf("resolve package %q: %w", selection.update.Name, err)
		}
		if len(resolved) == 0 {
			return AndroidKit{}, fmt.Errorf("Qt package %q contains no downloadable archives", selection.update.Name)
		}
		packageSelections = append(packageSelections, PackageSelection{
			Name:     strings.TrimSpace(selection.update.Name),
			Module:   selection.module,
			Archives: resolved,
		})
	}

	return AndroidKit{
		ABI:         abi,
		Destination: filepath.Join(destination, version.String(), packageArchitecture),
		Packages:    packageSelections,
	}, nil
}

func androidMetadataURL(repository Repository, version Version, abi AndroidABI) (string, error) {
	versionDirectory := fmt.Sprintf("qt%d_%s", version.Major, version.compact())
	abiDirectory := versionDirectory + "_" + abi.repositoryName()
	return url.JoinPath(repository.IndexURL(), versionDirectory, abiDirectory, "Updates.xml")
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
	if len(archiveNames) > 0 && len(extractPaths) == 0 && version != legacyAndroidArchiveLayoutVersion {
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
			Name:        logicalName,
			URL:         archiveURL,
			ChecksumURL: archiveURL + ".sha1",
			ExtractTo:   extractTo,
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
