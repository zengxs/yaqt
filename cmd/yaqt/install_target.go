package main

import (
	"fmt"

	"github.com/urfave/cli/v3"

	"github.com/zengxs/yaqt/internal/qtinstall"
	"github.com/zengxs/yaqt/internal/qtrepo"
)

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

var installTargetHandlers = map[qtrepo.Target]installTargetHandler{
	qtrepo.TargetDesktop: desktopInstallTargetHandler{},
	qtrepo.TargetAndroid: androidInstallTargetHandler{},
}

func installTargetHandlerFor(target qtrepo.Target) (installTargetHandler, error) {
	handler, ok := installTargetHandlers[target]
	if !ok {
		return nil, fmt.Errorf("install-qt supports only the desktop and Android targets")
	}
	return handler, nil
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
	architecture, err := qtrepo.ResolveDesktopArchitecture(
		repository.Host,
		command.String("arch"),
	)
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

type androidInstallTargetHandler struct{}

func (androidInstallTargetHandler) installRequest(
	command *cli.Command,
	repository qtrepo.Repository,
	version qtrepo.Version,
	destination string,
) (qtrepo.InstallRequest, error) {
	if command.String("arch") != "" {
		return qtrepo.InstallRequest{}, fmt.Errorf("--arch can be used only with --target desktop")
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
