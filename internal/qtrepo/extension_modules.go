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
	mainVariant packageVariant,
) ([]string, error) {
	mainMetadata := mainVariant.specification.metadata
	repositoryArchitecture := mainVariant.specification.extensionRepositoryArchitecture
	if repositoryArchitecture == "" {
		repositoryArchitecture = mainMetadata.packageArchitecture
	}
	modules := make([]string, 0, len(extensionModuleNames))
	for _, module := range extensionModuleNames {
		specification, err := extensionPackageVariantSpecification(
			request.Repository,
			request.Version,
			mainMetadata.packageArchitecture,
			repositoryArchitecture,
			module,
			mainVariant.installDirectory,
		)
		if err != nil {
			return nil, err
		}
		packages, found, err := c.fetchOptionalPackageUpdates(ctx, specification.metadata.url)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		if _, ok := packages[specification.metadata.basePackageName()]; !ok {
			continue
		}
		if _, err := resolvePackageVariant(specification, packages); err != nil {
			return nil, fmt.Errorf("validate extension module %q: %w", module, err)
		}
		modules = append(modules, module)
	}
	return modules, nil
}

func extensionPackageVariantSpecification(
	repository Repository,
	version Version,
	packageArchitecture,
	repositoryArchitecture,
	module,
	installDirectoryFallback string,
) (packageVariantSpecification, error) {
	metadataURL, err := url.JoinPath(
		repository.repositoryRootURL,
		"extensions",
		module,
		version.compact(),
		repositoryArchitecture,
		"Updates.xml",
	)
	if err != nil {
		return packageVariantSpecification{}, fmt.Errorf("construct %s extension metadata URL: %w", module, err)
	}
	return packageVariantSpecification{
		metadata: packageVariantMetadata{
			url:                 metadataURL,
			version:             version,
			packagePrefix:       fmt.Sprintf("extensions.%s.%s.", module, version.compact()),
			packageArchitecture: packageArchitecture,
			description:         "extension module " + module,
		},
		installDirectoryFallback: installDirectoryFallback,
	}, nil
}
