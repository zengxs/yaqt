package qtrepo

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/zengxs/yaqt/internal/httpclient"
)

var splitWindowsDesktopMetadataVersion = Version{Major: 6, Minor: 11}

type packageVariantMetadata struct {
	url                 string
	version             Version
	packagePrefix       string
	packageArchitecture string
	description         string
}

type packageVariantSpecification struct {
	metadata                        packageVariantMetadata
	installDirectoryFallback        string
	extensionRepositoryArchitecture string
}

func newPackageVariantMetadata(
	metadataURL string,
	version Version,
	packageArchitecture,
	description string,
) packageVariantMetadata {
	return packageVariantMetadata{
		url:                 metadataURL,
		version:             version,
		packagePrefix:       fmt.Sprintf("qt.qt%d.%s.", version.Major, version.compact()),
		packageArchitecture: packageArchitecture,
		description:         description,
	}
}

func desktopPackageVariantSpecification(
	repository Repository,
	version Version,
	architecture DesktopArchitecture,
) (packageVariantSpecification, error) {
	descriptor, err := architecture.descriptor()
	if err != nil {
		return packageVariantSpecification{}, err
	}
	metadataURL, err := desktopMetadataURL(repository, version, architecture, descriptor)
	if err != nil {
		return packageVariantSpecification{}, err
	}
	return packageVariantSpecification{
		metadata: newPackageVariantMetadata(
			metadataURL,
			version,
			string(architecture),
			"desktop architecture "+string(architecture),
		),
		installDirectoryFallback:        descriptor.installDirectory,
		extensionRepositoryArchitecture: descriptor.extensionRepositoryArchitecture,
	}, nil
}

func androidPackageVariantSpecification(
	repository Repository,
	version Version,
	abi AndroidABI,
) (packageVariantSpecification, error) {
	metadataURL, err := androidMetadataURL(repository, version, abi)
	if err != nil {
		return packageVariantSpecification{}, err
	}
	return packageVariantSpecification{
		metadata: newPackageVariantMetadata(
			metadataURL,
			version,
			abi.packageArchitecture(),
			"Android ABI "+string(abi),
		),
		installDirectoryFallback: abi.packageArchitecture(),
		extensionRepositoryArchitecture: fmt.Sprintf(
			"qt%d_%s_%s",
			version.Major,
			version.compact(),
			abi.repositoryName(),
		),
	}, nil
}

func (metadata packageVariantMetadata) basePackageName() string {
	return metadata.packagePrefix + metadata.packageArchitecture
}

func (metadata packageVariantMetadata) modulePackageNames(module string) [2]string {
	return [2]string{
		metadata.packagePrefix + "addons." + module + "." + metadata.packageArchitecture,
		metadata.packagePrefix + module + "." + metadata.packageArchitecture,
	}
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

func (c *Client) fetchPackageUpdates(
	ctx context.Context,
	metadataURL string,
) (map[string]packageUpdate, error) {
	metadata, err := c.fetchResource(ctx, metadataURL, packageMetadataResource)
	if err != nil {
		return nil, err
	}

	var updates updatesDocument
	if err := xml.Unmarshal(metadata, &updates); err != nil {
		return nil, fmt.Errorf("parse Qt package metadata %s: %w", metadataURL, err)
	}
	packages := make(map[string]packageUpdate, len(updates.Packages))
	for _, update := range updates.Packages {
		packages[strings.TrimSpace(update.Name)] = update
	}
	return packages, nil
}

func (c *Client) fetchOptionalPackageUpdates(
	ctx context.Context,
	metadataURL string,
) (map[string]packageUpdate, bool, error) {
	packages, err := c.fetchPackageUpdates(ctx, metadataURL)
	if err != nil {
		if httpclient.HasStatusCode(err, http.StatusNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return packages, true, nil
}

func androidMetadataURL(repository Repository, version Version, abi AndroidABI) (string, error) {
	versionDirectory := fmt.Sprintf("qt%d_%s", version.Major, version.compact())
	abiDirectory := versionDirectory + "_" + abi.repositoryName()
	return url.JoinPath(repository.IndexURL(), versionDirectory, abiDirectory, "Updates.xml")
}

func desktopMetadataURL(
	repository Repository,
	version Version,
	architecture DesktopArchitecture,
	descriptor desktopArchitectureDescriptor,
) (string, error) {
	versionDirectory := fmt.Sprintf("qt%d_%s", version.Major, version.compact())
	metadataDirectory := versionDirectory
	if repository.Host == HostWindows &&
		version.compare(splitWindowsDesktopMetadataVersion) >= 0 {
		if descriptor.repositorySuffix == "" {
			return "", fmt.Errorf(
				"desktop Qt architecture %q has no repository directory for Qt %s",
				architecture,
				version,
			)
		}
		metadataDirectory += "_" + descriptor.repositorySuffix
	}
	return url.JoinPath(
		repository.IndexURL(),
		versionDirectory,
		metadataDirectory,
		"Updates.xml",
	)
}
