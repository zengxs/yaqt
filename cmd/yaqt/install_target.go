package main

import (
	"fmt"

	"github.com/urfave/cli/v3"

	"github.com/zengxs/yaqt/internal/qtinstall"
	"github.com/zengxs/yaqt/internal/qtrepo"
)

type moduleRequestBuilder interface {
	moduleRequest(
		command *cli.Command,
		repository qtrepo.Repository,
		version qtrepo.Version,
	) (qtrepo.ModuleRequest, error)
}

type installTargetHandler interface {
	installRequest(
		command *cli.Command,
		repository qtrepo.Repository,
		version qtrepo.Version,
		destination string,
	) (qtrepo.InstallRequest, error)
	installPlanKits(qtrepo.InstallPlan) ([]installPlanKit, error)
	newRelocator(qtrepo.InstallPlan, string) (installRelocator, error)
	postInstallDescription() string
}

type targetHandlerSet struct {
	module  moduleRequestBuilder
	install installTargetHandler
}

var targetHandlers = map[qtrepo.Target]targetHandlerSet{
	qtrepo.TargetDesktop: {
		module:  desktopModuleRequestBuilder{},
		install: desktopInstallTargetHandler{},
	},
	qtrepo.TargetAndroid: {
		module:  androidModuleRequestBuilder{},
		install: androidInstallTargetHandler{},
	},
}

func moduleRequestBuilderFor(target qtrepo.Target) (moduleRequestBuilder, error) {
	handlers, err := targetHandlersFor(target)
	if err != nil {
		return nil, err
	}
	return handlers.module, nil
}

func installTargetHandlerFor(target qtrepo.Target) (installTargetHandler, error) {
	handlers, err := targetHandlersFor(target)
	if err != nil {
		return nil, err
	}
	return handlers.install, nil
}

func targetHandlersFor(target qtrepo.Target) (targetHandlerSet, error) {
	handlers, ok := targetHandlers[target]
	if !ok {
		return targetHandlerSet{}, fmt.Errorf(
			"unsupported Qt package target %q (choose desktop or android)",
			target,
		)
	}
	return handlers, nil
}

type desktopModuleRequestBuilder struct{}

func (desktopModuleRequestBuilder) moduleRequest(
	command *cli.Command,
	repository qtrepo.Repository,
	version qtrepo.Version,
) (qtrepo.ModuleRequest, error) {
	if command.String("abi") != "" {
		return qtrepo.ModuleRequest{}, fmt.Errorf("--abi can be used only with --target android")
	}
	architecture, err := desktopArchitectureFromCommand(command, repository)
	if err != nil {
		return qtrepo.ModuleRequest{}, err
	}
	return qtrepo.ModuleRequest{
		Repository:          repository,
		Version:             version,
		DesktopArchitecture: architecture,
	}, nil
}

type desktopInstallTargetHandler struct{}

func (desktopInstallTargetHandler) installRequest(
	command *cli.Command,
	repository qtrepo.Repository,
	version qtrepo.Version,
	destination string,
) (qtrepo.InstallRequest, error) {
	if len(command.StringSlice("abi")) != 0 {
		return qtrepo.InstallRequest{}, fmt.Errorf("--abi can be used only with --target android")
	}
	architecture, err := desktopArchitectureFromCommand(command, repository)
	if err != nil {
		return qtrepo.InstallRequest{}, err
	}
	return newInstallRequest(command, repository, version, destination, architecture, nil), nil
}

func (desktopInstallTargetHandler) installPlanKits(plan qtrepo.InstallPlan) ([]installPlanKit, error) {
	if plan.DesktopKit == nil {
		return nil, fmt.Errorf("desktop install plan contains no desktop kit")
	}
	return []installPlanKit{{
		architecture: string(plan.DesktopKit.Architecture),
		destination:  plan.DesktopKit.Destination,
		packages:     plan.DesktopKit.Packages,
	}}, nil
}

func (desktopInstallTargetHandler) newRelocator(
	plan qtrepo.InstallPlan,
	_ string,
) (installRelocator, error) {
	return qtinstall.NewDesktopRelocator(qtrepo.QtInstallationIdentity{
		Host:    plan.Host,
		Version: plan.Version,
	})
}

func (desktopInstallTargetHandler) postInstallDescription() string {
	return "relocate desktop Qt paths"
}

type androidModuleRequestBuilder struct{}

func (androidModuleRequestBuilder) moduleRequest(
	command *cli.Command,
	repository qtrepo.Repository,
	version qtrepo.Version,
) (qtrepo.ModuleRequest, error) {
	if err := rejectDesktopArchitecture(command); err != nil {
		return qtrepo.ModuleRequest{}, err
	}
	abiValue := command.String("abi")
	if abiValue == "" {
		return qtrepo.ModuleRequest{}, fmt.Errorf("--abi is required for the Android target")
	}
	abi, err := qtrepo.ParseAndroidABI(abiValue)
	if err != nil {
		return qtrepo.ModuleRequest{}, err
	}
	return qtrepo.ModuleRequest{
		Repository: repository,
		Version:    version,
		AndroidABI: abi,
	}, nil
}

type androidInstallTargetHandler struct{}

func (androidInstallTargetHandler) installRequest(
	command *cli.Command,
	repository qtrepo.Repository,
	version qtrepo.Version,
	destination string,
) (qtrepo.InstallRequest, error) {
	if err := rejectDesktopArchitecture(command); err != nil {
		return qtrepo.InstallRequest{}, err
	}
	abiValues := command.StringSlice("abi")
	if len(abiValues) == 0 {
		return qtrepo.InstallRequest{}, fmt.Errorf("at least one --abi is required for the Android target")
	}
	abis := make([]qtrepo.AndroidABI, 0, len(abiValues))
	for _, value := range abiValues {
		abi, err := qtrepo.ParseAndroidABI(value)
		if err != nil {
			return qtrepo.InstallRequest{}, err
		}
		abis = append(abis, abi)
	}
	return newInstallRequest(command, repository, version, destination, "", abis), nil
}

func (androidInstallTargetHandler) installPlanKits(plan qtrepo.InstallPlan) ([]installPlanKit, error) {
	kits := make([]installPlanKit, 0, len(plan.AndroidKits))
	for _, kit := range plan.AndroidKits {
		kits = append(kits, installPlanKit{
			architecture: string(kit.ABI),
			destination:  kit.Destination,
			packages:     kit.Packages,
		})
	}
	if len(kits) == 0 {
		return nil, fmt.Errorf("Android install plan contains no Android kits")
	}
	return kits, nil
}

func (androidInstallTargetHandler) newRelocator(
	plan qtrepo.InstallPlan,
	qtRoot string,
) (installRelocator, error) {
	if plan.HostQt == nil {
		return nil, fmt.Errorf("Android install plan has no host Qt identity")
	}
	return qtinstall.NewAndroidRelocator(*plan.HostQt, qtRoot)
}

func (androidInstallTargetHandler) postInstallDescription() string {
	return "relocate Android Qt paths and connect each kit to the matching host Qt"
}

func desktopArchitectureFromCommand(
	command *cli.Command,
	repository qtrepo.Repository,
) (qtrepo.DesktopArchitecture, error) {
	return qtrepo.ResolveDesktopArchitecture(repository.Host, command.String("arch"))
}

func rejectDesktopArchitecture(command *cli.Command) error {
	if command.String("arch") != "" {
		return fmt.Errorf("--arch can be used only with --target desktop")
	}
	return nil
}

func newInstallRequest(
	command *cli.Command,
	repository qtrepo.Repository,
	version qtrepo.Version,
	destination string,
	desktopArchitecture qtrepo.DesktopArchitecture,
	abis []qtrepo.AndroidABI,
) qtrepo.InstallRequest {
	return qtrepo.InstallRequest{
		Repository:          repository,
		Version:             version,
		DesktopArchitecture: desktopArchitecture,
		AndroidABIs:         abis,
		Modules:             command.StringSlice("module"),
		Destination:         destination,
	}
}

func defaultInstallRelocatorFactory(
	plan qtrepo.InstallPlan,
	qtRoot string,
) (installRelocator, error) {
	handler, err := installTargetHandlerFor(plan.Target)
	if err != nil {
		return nil, err
	}
	return handler.newRelocator(plan, qtRoot)
}
