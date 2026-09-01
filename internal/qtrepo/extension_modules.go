package qtrepo

import (
	"context"
	"fmt"
	"net/url"
)

var extensionModuleNames = []string{"qtwebengine", "qtpdf"}

func (c *Client) listExtensionModules(
	ctx context.Context,
	request ModuleRequest,
	mainMetadata packageVariantMetadata,
) ([]string, error) {
	repositoryArchitecture, err := extensionRepositoryArchitecture(request, mainMetadata)
	if err != nil {
		return nil, err
	}
	modules := make([]string, 0, len(extensionModuleNames))
	for _, module := range extensionModuleNames {
		metadata, err := extensionPackageVariantMetadata(
			request.Repository,
			request.Version,
			mainMetadata.packageArchitecture,
			repositoryArchitecture,
			module,
		)
		if err != nil {
			return nil, err
		}
		packages, found, err := c.fetchOptionalPackageUpdates(ctx, metadata.url)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		update, ok := packages[metadata.basePackageName()]
		if !ok {
			continue
		}
		if _, err := resolvePackageSelections(
			metadata,
			moduleValidationDestination,
			request.Version,
			[]selectedPackage{{update: update, module: module}},
		); err != nil {
			return nil, fmt.Errorf("validate extension module %q: %w", module, err)
		}
		modules = append(modules, module)
	}
	return modules, nil
}

func extensionPackageVariantMetadata(
	repository Repository,
	version Version,
	packageArchitecture,
	repositoryArchitecture,
	module string,
) (packageVariantMetadata, error) {
	metadataURL, err := url.JoinPath(
		repository.repositoryRootURL,
		"extensions",
		module,
		version.compact(),
		repositoryArchitecture,
		"Updates.xml",
	)
	if err != nil {
		return packageVariantMetadata{}, fmt.Errorf("construct %s extension metadata URL: %w", module, err)
	}
	return packageVariantMetadata{
		url:                 metadataURL,
		packagePrefix:       fmt.Sprintf("extensions.%s.%s.", module, version.compact()),
		packageArchitecture: packageArchitecture,
		description:         "extension module " + module,
	}, nil
}

func extensionRepositoryArchitecture(
	request ModuleRequest,
	mainMetadata packageVariantMetadata,
) (string, error) {
	if request.Repository.Target == TargetAndroid {
		return fmt.Sprintf(
			"qt%d_%s_%s",
			request.Version.Major,
			request.Version.compact(),
			request.AndroidABI.repositoryName(),
		), nil
	}
	if request.Repository.Target == TargetDesktop {
		return DesktopArchitecture(mainMetadata.packageArchitecture).
			extensionRepositoryArchitecture()
	}
	return mainMetadata.packageArchitecture, nil
}
