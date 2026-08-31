package qtrepo

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
)

const iosPackageArchitecture = "ios"

func iosPackageVariantMetadata(
	repository Repository,
	version Version,
) (packageVariantMetadata, error) {
	metadataURL, err := iosMetadataURL(repository, version)
	if err != nil {
		return packageVariantMetadata{}, err
	}
	return newPackageVariantMetadata(
		metadataURL,
		version,
		iosPackageArchitecture,
		"iOS",
	), nil
}

func iosMetadataURL(repository Repository, version Version) (string, error) {
	versionDirectory := fmt.Sprintf("qt%d_%s", version.Major, version.compact())
	return url.JoinPath(
		repository.IndexURL(),
		versionDirectory,
		versionDirectory,
		"Updates.xml",
	)
}

func (c *Client) resolveIOSInstall(
	ctx context.Context,
	request InstallRequest,
	modules []string,
	destination string,
) (InstallPlan, error) {
	if request.DesktopArchitecture != "" {
		return InstallPlan{}, fmt.Errorf("desktop architecture cannot be used with the iOS target")
	}
	if len(request.AndroidABIs) != 0 {
		return InstallPlan{}, fmt.Errorf("Android ABIs cannot be used with the iOS target")
	}

	kit, err := c.resolveIOSKit(
		ctx,
		request.Repository,
		request.Version,
		modules,
		destination,
	)
	if err != nil {
		return InstallPlan{}, err
	}
	return InstallPlan{
		Version: request.Version,
		Host:    request.Repository.Host,
		Target:  TargetIOS,
		HostQt: &QtInstallationIdentity{
			Host:    request.Repository.Host,
			Version: request.Version,
		},
		IOSKit: &kit,
	}, nil
}

func (c *Client) resolveIOSKit(
	ctx context.Context,
	repository Repository,
	version Version,
	modules []string,
	destination string,
) (IOSKit, error) {
	metadata, err := iosPackageVariantMetadata(repository, version)
	if err != nil {
		return IOSKit{}, err
	}
	packages, err := c.fetchPackageUpdates(ctx, metadata.url)
	if err != nil {
		return IOSKit{}, err
	}
	selected, err := selectPackageUpdates(packages, metadata, modules, version)
	if err != nil {
		return IOSKit{}, err
	}
	packageSelections, err := resolvePackageSelections(
		metadata,
		destination,
		version,
		selected,
	)
	if err != nil {
		return IOSKit{}, err
	}
	return IOSKit{
		Destination: filepath.Join(destination, version.String(), iosPackageArchitecture),
		Packages:    packageSelections,
	}, nil
}

func iosModuleVariant(request ModuleRequest) (packageVariantMetadata, error) {
	if request.DesktopArchitecture != "" {
		return packageVariantMetadata{}, fmt.Errorf("desktop architecture cannot be used with the iOS target")
	}
	if request.AndroidABI != "" {
		return packageVariantMetadata{}, fmt.Errorf("Android ABI cannot be used with the iOS target")
	}
	return iosPackageVariantMetadata(request.Repository, request.Version)
}
