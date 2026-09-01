package qtrepo

import "context"

type installTargetPlanner func(
	*Client,
	context.Context,
	InstallRequest,
	[]string,
	string,
) (InstallPlan, error)

type moduleVariantResolver func(ModuleRequest) (packageVariantSpecification, error)

type packageTargetOperations struct {
	planInstall          installTargetPlanner
	resolveModuleVariant moduleVariantResolver
}

var packageTargets = map[Target]packageTargetOperations{
	TargetDesktop: {
		planInstall:          (*Client).resolveDesktopInstall,
		resolveModuleVariant: desktopModuleVariant,
	},
	TargetAndroid: {
		planInstall:          (*Client).resolveAndroidInstall,
		resolveModuleVariant: androidModuleVariant,
	},
	TargetIOS: {
		planInstall:          (*Client).resolveIOSInstall,
		resolveModuleVariant: iosModuleVariant,
	},
}
