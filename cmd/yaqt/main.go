package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/zengxs/yaqt/internal/buildinfo"
	"github.com/zengxs/yaqt/internal/cache"
	"github.com/zengxs/yaqt/internal/qtinstall"
	"github.com/zengxs/yaqt/internal/qtrepo"
)

func main() {
	defaultHost, err := qtrepo.CurrentHost()
	if err != nil {
		fmt.Fprintf(os.Stderr, "yaqt: %v\n", err)
		os.Exit(1)
	}

	client := qtrepo.NewCachedClient(&http.Client{Timeout: 10 * time.Second})
	installDependencies := installCommandDependencies{
		resolver: client,
		fetcherFactory: func(cacheDir string) (archiveFetcher, error) {
			return qtinstall.NewArchiveStore(nil, cacheDir)
		},
		extractor:        qtinstall.SevenZipExtractor{},
		relocatorFactory: defaultInstallRelocatorFactory,
	}
	command := newCommand(
		commandDependencies{
			versionLister:      client,
			architectureLister: client,
			moduleLister:       client,
			install:            installDependencies,
		},
		defaultHost,
		os.Stdout,
		os.Stderr,
	)
	if err := command.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "yaqt: %v\n", err)
		os.Exit(1)
	}
}

type versionLister interface {
	ListVersions(context.Context, qtrepo.Repository) ([]qtrepo.Version, error)
}

type installResolver interface {
	ResolveInstall(context.Context, qtrepo.InstallRequest) (qtrepo.InstallPlan, error)
}

type archiveFetcher = qtinstall.ArchiveFetcher

type archiveFetcherFactory = qtinstall.ArchiveFetcherFactory

type archiveExtractor = qtinstall.ArchiveExtractor

type installRelocator = qtinstall.Relocator

type installRelocatorFactory func(qtrepo.InstallPlan, string) (installRelocator, error)

type installCommandDependencies struct {
	resolver         installResolver
	fetcherFactory   archiveFetcherFactory
	extractor        archiveExtractor
	relocatorFactory installRelocatorFactory
}

type commandDependencies struct {
	versionLister      versionLister
	architectureLister architectureLister
	moduleLister       moduleLister
	install            installCommandDependencies
}

type installExecutionMode uint8

const (
	installExecutionModeInstall installExecutionMode = iota + 1
	installExecutionModeDryRun
	installExecutionModeDownloadOnly
	installExecutionModeExtractOnly
)

func newCommand(
	dependencies commandDependencies,
	defaultHost qtrepo.Host,
	output,
	errorOutput io.Writer,
) *cli.Command {
	return &cli.Command{
		Name:      "yaqt",
		Usage:     "Install Qt SDK components non-interactively",
		Version:   buildinfo.Version,
		Writer:    output,
		ErrWriter: errorOutput,
		Suggest:   true,
		Commands: []*cli.Command{
			newListQtCommand(dependencies.versionLister, defaultHost, output),
			newListArchitecturesCommand(dependencies.architectureLister, defaultHost, output),
			newListModulesCommand(dependencies.moduleLister, defaultHost, output),
			newInstallQtCommand(
				dependencies.install,
				defaultHost,
				output,
			),
		},
	}
}

func newListQtCommand(lister versionLister, defaultHost qtrepo.Host, output io.Writer) *cli.Command {
	return withRepositoryCache(&cli.Command{
		Name:        "list-qt",
		Usage:       "List supported Qt versions available in a repository",
		Description: "The command reads the selected repository's directory index and prints stable Qt 6.8.0 or newer releases, one per line in ascending order.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "host",
				Value: string(defaultHost),
				Usage: "Host `platform`: windows, windows_arm64, mac, linux, or linux_arm64",
			},
			&cli.StringFlag{
				Name:  "target",
				Value: string(qtrepo.TargetDesktop),
				Usage: "Qt `target`: desktop, android, winrt, ios, wasm, or qt",
			},
			&cli.StringFlag{
				Name:  "base-url",
				Value: qtrepo.DefaultBaseURL,
				Usage: "Qt download server or mirror `URL`",
			},
		},
		Action: func(ctx context.Context, command *cli.Command) error {
			if command.NArg() != 0 {
				return fmt.Errorf("list-qt does not accept positional arguments")
			}

			repository, err := repositoryFromCommand(command)
			if err != nil {
				return err
			}
			versions, err := lister.ListVersions(ctx, repository)
			if err != nil {
				return err
			}
			for _, version := range versions {
				if _, err := fmt.Fprintln(output, version); err != nil {
					return fmt.Errorf("write version list: %w", err)
				}
			}
			return nil
		},
	})
}

func newInstallQtCommand(
	dependencies installCommandDependencies,
	defaultHost qtrepo.Host,
	output io.Writer,
) *cli.Command {
	return withRepositoryCache(&cli.Command{
		Name:      "install-qt",
		Usage:     "Install or incrementally update a Qt SDK installation",
		ArgsUsage: "VERSION",
		Description: "Resolve and incrementally install native desktop, Android, or iOS Qt kits. " +
			"Mobile installation uses an existing matching desktop Qt. " +
			"The command can also stop after planning, downloading, or extraction.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "host",
				Value: string(defaultHost),
				Usage: "Host `platform`: windows, windows_arm64, mac, linux, or linux_arm64",
			},
			&cli.StringFlag{
				Name:     "target",
				Usage:    "Qt `target`: desktop, android, or ios",
				Required: true,
			},
			&cli.StringFlag{
				Name:  "arch",
				Usage: "Desktop Qt `architecture`; defaults to the native architecture for the selected host",
			},
			&cli.StringSliceFlag{
				Name:  "abi",
				Usage: "Android `ABI`: arm64-v8a, armeabi-v7a, x86, or x86_64; may be repeated",
			},
			&cli.StringSliceFlag{
				Name:    "module",
				Aliases: []string{"m"},
				Usage:   "Additional Qt `module`; may be repeated",
			},
			&cli.StringFlag{
				Name:     "root",
				Usage:    "Qt installation root `DIR`; must not include the version directory",
				Required: true,
			},
			&cli.StringFlag{
				Name:  "base-url",
				Value: qtrepo.DefaultBaseURL,
				Usage: "Qt download server or mirror `URL`",
			},
			&cli.BoolFlag{
				Name:  "dry-run",
				Usage: "Print the installation plan without downloading archives or changing the Qt installation",
			},
			&cli.BoolFlag{
				Name:  "download-only",
				Usage: "Download and verify archives without extracting them",
			},
			&cli.BoolFlag{
				Name:  "extract-only",
				Usage: "Download, verify, and extract archives without applying path relocation",
			},
		},
		Action: func(ctx context.Context, command *cli.Command) error {
			if command.NArg() != 1 {
				return fmt.Errorf("install-qt requires exactly one Qt version")
			}
			mode, err := requestedInstallExecutionMode(command)
			if err != nil {
				return err
			}
			version, err := qtrepo.ParseVersion(command.Args().First())
			if err != nil {
				return err
			}
			installRoot, err := qtinstall.ResolveInstallRoot(command.String("root"), version)
			if err != nil {
				return err
			}
			repository, err := repositoryFromCommand(command)
			if err != nil {
				return err
			}
			targetHandler, err := installTargetHandlerFor(repository.Target)
			if err != nil {
				return err
			}
			if mode == installExecutionModeInstall {
				if repository.Host != defaultHost {
					return fmt.Errorf(
						"complete installation host %q does not match the current host %q; use --download-only or --extract-only for cross-host materialization",
						repository.Host,
						defaultHost,
					)
				}
				if dependencies.relocatorFactory == nil {
					return fmt.Errorf("Qt post-install relocator is not configured")
				}
			}

			request, err := targetHandler.installRequest(command, repository, version, installRoot)
			if err != nil {
				return err
			}
			cacheRoot, ok := cache.RootFromContext(ctx)
			if !ok {
				return fmt.Errorf("yaqt cache root is not configured")
			}
			plan, err := dependencies.resolver.ResolveInstall(ctx, request)
			if err != nil {
				return err
			}
			if mode == installExecutionModeDryRun {
				reconciliation, err := reconcileInstallPlan(plan, installRoot)
				if err != nil {
					return fmt.Errorf("reconcile Qt installation: %w", err)
				}
				return printInstallPlan(output, plan, reconciliation)
			}
			kits, err := installKitRequests(plan)
			if err != nil {
				return err
			}
			materializationMode, err := qtinstallExecutionMode(mode)
			if err != nil {
				return err
			}
			var relocatorFactory qtinstall.RelocatorFactory
			if dependencies.relocatorFactory != nil {
				relocatorFactory = func() (qtinstall.Relocator, error) {
					return dependencies.relocatorFactory(plan, installRoot)
				}
			}
			installer := qtinstall.NewInstaller(
				dependencies.fetcherFactory,
				dependencies.extractor,
				relocatorFactory,
			)
			return installer.Materialize(
				ctx,
				output,
				qtinstall.MaterializeRequest{
					Identity: qtinstall.InstallationIdentity{
						Version: plan.Version,
						Host:    plan.Host,
						Target:  plan.Target,
					},
					Root:     installRoot,
					CacheDir: cacheRoot,
					Kits:     kits,
					Mode:     materializationMode,
				},
			)
		},
	})
}

func withRepositoryCache(command *cli.Command) *cli.Command {
	command.Flags = append(command.Flags, &cli.StringFlag{
		Name: "cache-dir",
		Usage: "yaqt cache root `DIR`; defaults to YAQT_CACHE_DIR or the operating " +
			"system cache",
	})
	previousBefore := command.Before
	command.Before = func(ctx context.Context, command *cli.Command) (context.Context, error) {
		if previousBefore != nil {
			var err error
			ctx, err = previousBefore(ctx, command)
			if err != nil {
				return nil, err
			}
		}
		root, err := cache.ResolveRoot(command.String("cache-dir"))
		if err != nil {
			return nil, err
		}
		return cache.ContextWithRoot(ctx, root), nil
	}
	return command
}

func requestedInstallExecutionMode(command *cli.Command) (installExecutionMode, error) {
	candidates := []struct {
		enabled bool
		mode    installExecutionMode
	}{
		{enabled: command.Bool("dry-run"), mode: installExecutionModeDryRun},
		{enabled: command.Bool("download-only"), mode: installExecutionModeDownloadOnly},
		{enabled: command.Bool("extract-only"), mode: installExecutionModeExtractOnly},
	}

	var selected installExecutionMode
	for _, candidate := range candidates {
		if !candidate.enabled {
			continue
		}
		if selected != 0 {
			return 0, fmt.Errorf("--dry-run, --download-only, and --extract-only are mutually exclusive")
		}
		selected = candidate.mode
	}
	if selected == 0 {
		return installExecutionModeInstall, nil
	}
	return selected, nil
}

func repositoryFromCommand(command *cli.Command) (qtrepo.Repository, error) {
	host, err := qtrepo.ParseHost(command.String("host"))
	if err != nil {
		return qtrepo.Repository{}, err
	}
	target, err := qtrepo.ParseTarget(command.String("target"))
	if err != nil {
		return qtrepo.Repository{}, err
	}
	return qtrepo.NewRepository(command.String("base-url"), host, target)
}

func printInstallPlan(
	output io.Writer,
	plan qtrepo.InstallPlan,
	reconciliation qtinstall.Reconciliation,
) error {
	write := func(format string, arguments ...any) error {
		_, err := fmt.Fprintf(output, format, arguments...)
		return err
	}

	if err := write("Qt %s for %s on %s\n", plan.Version, plan.Target, plan.Host); err != nil {
		return fmt.Errorf("write install plan: %w", err)
	}
	if plan.HostQt != nil {
		if err := write(
			"Host Qt requirement: %s desktop %s\n",
			plan.HostQt.Host,
			plan.HostQt.Version,
		); err != nil {
			return fmt.Errorf("write install plan: %w", err)
		}
	}
	kits, err := installPlanKits(plan)
	if err != nil {
		return err
	}
	for kitIndex, kit := range kits {
		if err := write("\n%s -> %s\n", kit.architecture, kit.destination); err != nil {
			return fmt.Errorf("write install plan: %w", err)
		}
		for packageIndex, packageSelection := range kit.packages {
			selection := "base package"
			if packageSelection.Module != "" {
				selection = "module " + packageSelection.Module
			}
			if err := write("  %s: %s\n", selection, packageSelection.Name); err != nil {
				return fmt.Errorf("write install plan: %w", err)
			}
			if err := write(
				"    action: %s\n",
				reconciliation.Kits[kitIndex].Packages[packageIndex].Action,
			); err != nil {
				return fmt.Errorf("write install plan: %w", err)
			}
			for _, archive := range packageSelection.Archives {
				if err := write(
					"    %s\n      download: %s\n      checksum (%s): %s\n      extract to: %s\n",
					archive.Name,
					archive.URL,
					archive.Checksum.Algorithm,
					archive.Checksum.URL,
					archive.ExtractTo,
				); err != nil {
					return fmt.Errorf("write install plan: %w", err)
				}
			}
		}
	}
	targetHandler, err := installTargetHandlerFor(plan.Target)
	if err != nil {
		return err
	}
	if err := write("\nPost-install: %s.\n", targetHandler.postInstallDescription()); err != nil {
		return fmt.Errorf("write install plan: %w", err)
	}
	if reconciliation.IsSatisfied() {
		if err := write(
			"Qt %s for %s is already satisfied.\n",
			plan.Version,
			plan.Target,
		); err != nil {
			return fmt.Errorf("write install plan: %w", err)
		}
	}
	return nil
}

func reconcileInstallPlan(
	plan qtrepo.InstallPlan,
	installRoot string,
) (qtinstall.Reconciliation, error) {
	requestedKits, err := installKitRequests(plan)
	if err != nil {
		return qtinstall.Reconciliation{}, err
	}
	return qtinstall.ReconcileInstallation(
		qtinstall.InstallationIdentity{
			Version: plan.Version,
			Host:    plan.Host,
			Target:  plan.Target,
		},
		installRoot,
		requestedKits,
	)
}

func installKitRequests(plan qtrepo.InstallPlan) ([]qtinstall.KitRequest, error) {
	kits, err := installPlanKits(plan)
	if err != nil {
		return nil, err
	}
	requestedKits := make([]qtinstall.KitRequest, 0, len(kits))
	for _, kit := range kits {
		requestedKits = append(requestedKits, qtinstall.KitRequest{
			Architecture: kit.architecture,
			Destination:  kit.destination,
			Packages:     kit.packages,
		})
	}
	return requestedKits, nil
}

func qtinstallExecutionMode(mode installExecutionMode) (qtinstall.ExecutionMode, error) {
	switch mode {
	case installExecutionModeInstall:
		return qtinstall.ExecutionModeInstall, nil
	case installExecutionModeDownloadOnly:
		return qtinstall.ExecutionModeDownloadOnly, nil
	case installExecutionModeExtractOnly:
		return qtinstall.ExecutionModeExtractOnly, nil
	default:
		return 0, fmt.Errorf("unsupported installation execution mode %d", mode)
	}
}

type installPlanKit struct {
	architecture string
	destination  string
	packages     []qtrepo.PackageSelection
}

func installPlanKits(plan qtrepo.InstallPlan) ([]installPlanKit, error) {
	handler, err := installTargetHandlerFor(plan.Target)
	if err != nil {
		return nil, err
	}
	return handler.installPlanKits(plan)
}
