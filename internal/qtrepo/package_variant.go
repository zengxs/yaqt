package qtrepo

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
)

const variantValidationDestination = "."

// ArchitectureRequest selects the repository release whose package
// architectures should be listed.
type ArchitectureRequest struct {
	Repository Repository
	Version    Version
}

type packageVariant struct {
	specification    packageVariantSpecification
	packages         map[string]packageUpdate
	installDirectory string
}

type packageVariantInstall struct {
	destination string
	packages    []PackageSelection
}

// ListArchitectures returns package architecture names advertised with usable
// archive metadata by one repository release, sorted in ascending order.
func (c *Client) ListArchitectures(
	ctx context.Context,
	request ArchitectureRequest,
) ([]string, error) {
	if err := validateSupportedVersion(request.Version); err != nil {
		return nil, err
	}
	if !supportsArchitectureListing(request.Repository.Target) {
		return nil, fmt.Errorf(
			"architecture listing does not support target %q",
			request.Repository.Target,
		)
	}

	metadataURLs, err := c.architectureMetadataURLs(ctx, request.Repository, request.Version)
	if err != nil {
		return nil, err
	}
	variants := make(map[string]string)
	for _, metadataURL := range metadataURLs {
		packages, err := c.fetchPackageUpdates(ctx, metadataURL)
		if err != nil {
			return nil, fmt.Errorf("read architecture metadata %s: %w", metadataURL, err)
		}
		discovered, err := packageVariantsFromMetadata(metadataURL, request.Version, packages)
		if err != nil {
			return nil, err
		}
		for _, variant := range discovered {
			architecture := variant.specification.metadata.packageArchitecture
			if previousURL, ok := variants[architecture]; ok && previousURL != metadataURL {
				return nil, fmt.Errorf(
					"Qt architecture %q is advertised by both %s and %s",
					architecture,
					previousURL,
					metadataURL,
				)
			}
			variants[architecture] = metadataURL
		}
	}

	architectures := make([]string, 0, len(variants))
	for architecture := range variants {
		architectures = append(architectures, architecture)
	}
	slices.Sort(architectures)
	if len(architectures) == 0 {
		return nil, fmt.Errorf(
			"Qt %s repository for %s contains no usable Qt package architectures",
			request.Version,
			request.Repository.Target,
		)
	}
	return architectures, nil
}

func supportsArchitectureListing(target Target) bool {
	switch target {
	case TargetDesktop, TargetAndroid, TargetIOS, TargetWASM:
		return true
	default:
		return false
	}
}

func (c *Client) architectureMetadataURLs(
	ctx context.Context,
	repository Repository,
	version Version,
) ([]string, error) {
	versionDirectory := fmt.Sprintf("qt%d_%s", version.Major, version.compact())
	outerURL, err := url.JoinPath(repository.IndexURL(), versionDirectory)
	if err != nil {
		return nil, fmt.Errorf("construct Qt %s repository URL: %w", version, err)
	}
	outerURL = strings.TrimSuffix(outerURL, "/") + "/"
	index, err := c.fetchResource(ctx, outerURL, repositoryIndexResource)
	if err != nil {
		return nil, err
	}
	children, err := parseRepositoryChildNames(bytes.NewReader(index))
	if err != nil {
		return nil, fmt.Errorf("parse Qt repository index %s: %w", outerURL, err)
	}

	metadataURLs := make([]string, 0, len(children))
	for _, child := range children {
		if child != versionDirectory && !strings.HasPrefix(child, versionDirectory+"_") {
			continue
		}
		entry, ok := parseRepositoryEntry(child)
		if !ok || entry.Version != version || !repositoryExtensionCanContainArchitecture(entry.Extension) {
			continue
		}
		metadataURL, err := url.JoinPath(outerURL, child, "Updates.xml")
		if err != nil {
			return nil, fmt.Errorf("construct architecture metadata URL: %w", err)
		}
		metadataURLs = append(metadataURLs, metadataURL)
	}
	if len(metadataURLs) == 0 {
		return nil, fmt.Errorf(
			"Qt repository index %s contains no package variants for Qt %s",
			outerURL,
			version,
		)
	}
	return metadataURLs, nil
}

func repositoryExtensionCanContainArchitecture(extension string) bool {
	if isSourceContentRepositoryExtension(extension) {
		return false
	}
	return !strings.Contains(extension, "preview") && !strings.Contains(extension, "backup")
}

func packageVariantsFromMetadata(
	metadataURL string,
	version Version,
	packages map[string]packageUpdate,
) ([]packageVariant, error) {
	prefix := fmt.Sprintf("qt.qt%d.%s.", version.Major, version.compact())
	architectures := make([]string, 0)
	for packageName, update := range packages {
		architecture, ok := strings.CutPrefix(packageName, prefix)
		if !ok || strings.Contains(architecture, ".") ||
			!validPackageIdentifier(architecture) ||
			len(splitMetadataList(update.DownloadableArchives)) == 0 {
			continue
		}
		architectures = append(architectures, architecture)
	}
	slices.Sort(architectures)

	variants := make([]packageVariant, 0, len(architectures))
	for _, architecture := range architectures {
		specification := packageVariantSpecification{
			metadata: newPackageVariantMetadata(
				metadataURL,
				version,
				architecture,
				"architecture "+architecture,
			),
		}
		variant, err := resolvePackageVariant(specification, packages)
		if err != nil {
			return nil, fmt.Errorf("validate Qt architecture %q: %w", architecture, err)
		}
		variants = append(variants, variant)
	}
	return variants, nil
}

func (c *Client) resolvePackageVariant(
	ctx context.Context,
	specification packageVariantSpecification,
) (packageVariant, error) {
	packages, err := c.fetchPackageUpdates(ctx, specification.metadata.url)
	if err != nil {
		return packageVariant{}, err
	}
	return resolvePackageVariant(specification, packages)
}

func resolvePackageVariant(
	specification packageVariantSpecification,
	packages map[string]packageUpdate,
) (packageVariant, error) {
	metadata := specification.metadata
	basePackage, ok := packages[metadata.basePackageName()]
	if !ok {
		return packageVariant{}, fmt.Errorf(
			"Qt %s metadata contains no base package for %s",
			metadata.version,
			metadata.description,
		)
	}
	if _, err := resolvePackageSelections(
		metadata,
		variantValidationDestination,
		metadata.version,
		[]selectedPackage{{update: basePackage}},
	); err != nil {
		return packageVariant{}, err
	}
	installDirectory, err := packageInstallDirectory(
		basePackage,
		metadata.version,
		specification.installDirectoryFallback,
	)
	if err != nil {
		return packageVariant{}, fmt.Errorf(
			"resolve install directory for %s: %w",
			metadata.description,
			err,
		)
	}
	return packageVariant{
		specification:    specification,
		packages:         packages,
		installDirectory: installDirectory,
	}, nil
}

func (c *Client) resolvePackageVariantInstall(
	ctx context.Context,
	specification packageVariantSpecification,
	modules []string,
	destination string,
) (packageVariantInstall, error) {
	variant, err := c.resolvePackageVariant(ctx, specification)
	if err != nil {
		return packageVariantInstall{}, err
	}
	packageSelections, err := variant.resolvePackageSelections(destination, modules)
	if err != nil {
		return packageVariantInstall{}, err
	}
	kitDestination, err := variant.destination(destination)
	if err != nil {
		return packageVariantInstall{}, err
	}
	return packageVariantInstall{
		destination: kitDestination,
		packages:    packageSelections,
	}, nil
}

func (variant packageVariant) destination(root string) (string, error) {
	if variant.installDirectory == "" {
		return "", fmt.Errorf(
			"Qt package metadata contains no install directory for %s",
			variant.specification.metadata.description,
		)
	}
	return filepath.Join(
		root,
		variant.specification.metadata.version.String(),
		variant.installDirectory,
	), nil
}

func (variant packageVariant) resolvePackageSelections(
	destination string,
	modules []string,
) ([]PackageSelection, error) {
	metadata := variant.specification.metadata
	selected, err := selectPackageUpdates(
		variant.packages,
		metadata,
		modules,
		metadata.version,
	)
	if err != nil {
		return nil, err
	}
	return resolvePackageSelections(
		metadata,
		destination,
		metadata.version,
		selected,
	)
}

func packageInstallDirectory(
	basePackage packageUpdate,
	version Version,
	fallback string,
) (string, error) {
	extractPaths, err := extractPathsByArchive(basePackage.Operations)
	if err != nil {
		return "", err
	}
	if len(extractPaths) == 0 {
		return fallback, nil
	}

	archives := splitMetadataList(basePackage.DownloadableArchives)
	if len(archives) == 0 {
		return fallback, nil
	}
	selectedArchive := archives[0]
	for _, archive := range archives {
		if logicalArchiveName(archive) == "qtbase" {
			selectedArchive = archive
			break
		}
	}
	relativePath, ok := extractPaths[selectedArchive]
	if !ok {
		return "", fmt.Errorf(
			"metadata contains no Extract operation for base archive %q",
			selectedArchive,
		)
	}
	// Archives extracted at @TargetDir@ may carry the version and kit
	// directories in their own entry names, so repository metadata alone cannot
	// improve on the target-specific fallback in that case.
	if relativePath == "" {
		return fallback, nil
	}
	prefix := version.String() + "/"
	if !strings.HasPrefix(relativePath, prefix) {
		return "", fmt.Errorf(
			"base archive Extract destination %q is not inside the Qt %s directory",
			relativePath,
			version,
		)
	}
	installDirectory, _, _ := strings.Cut(strings.TrimPrefix(relativePath, prefix), "/")
	if !safeURLSegment(installDirectory) {
		return "", fmt.Errorf("metadata contains an unsafe install directory %q", installDirectory)
	}
	return installDirectory, nil
}

func logicalArchiveName(archiveName string) string {
	logicalName, _, _ := strings.Cut(archiveName, "-")
	return strings.TrimSuffix(logicalName, ".7z")
}
