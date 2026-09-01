package qtrepo

import (
	"context"
	"fmt"
	"net/url"
)

const iosPackageArchitecture = "ios"

func iosPackageVariantSpecification(
	repository Repository,
	version Version,
) (packageVariantSpecification, error) {
	metadataURL, err := iosMetadataURL(repository, version)
	if err != nil {
		return packageVariantSpecification{}, err
	}
	return packageVariantSpecification{
		metadata: newPackageVariantMetadata(
			metadataURL,
			version,
			iosPackageArchitecture,
			"iOS",
		),
		installDirectoryFallback: iosPackageArchitecture,
	}, nil
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
	specification, err := iosPackageVariantSpecification(repository, version)
	if err != nil {
		return IOSKit{}, err
	}
	install, err := c.resolvePackageVariantInstall(ctx, specification, modules, destination)
	if err != nil {
		return IOSKit{}, err
	}
	return IOSKit{
		Destination: install.destination,
		Packages:    install.packages,
	}, nil
}

func iosModuleVariant(request ModuleRequest) (packageVariantSpecification, error) {
	if request.DesktopArchitecture != "" {
		return packageVariantSpecification{}, fmt.Errorf("desktop architecture cannot be used with the iOS target")
	}
	if request.AndroidABI != "" {
		return packageVariantSpecification{}, fmt.Errorf("Android ABI cannot be used with the iOS target")
	}
	return iosPackageVariantSpecification(request.Repository, request.Version)
}
