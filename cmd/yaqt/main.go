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
	"github.com/zengxs/yaqt/internal/qtinstall"
	"github.com/zengxs/yaqt/internal/qtrepo"
)

func main() {
	defaultHost, err := qtrepo.CurrentHost()
	if err != nil {
		fmt.Fprintf(os.Stderr, "yaqt: %v\n", err)
		os.Exit(1)
	}

	client := qtrepo.NewClient(&http.Client{Timeout: 30 * time.Second})
	installDependencies := installCommandDependencies{
		resolver: client,
		fetcherFactory: func(cacheDir string) (archiveFetcher, error) {
			return qtinstall.NewArchiveStore(nil, cacheDir)
		},
		extractor:        qtinstall.SevenZipExtractor{},
		relocatorFactory: defaultInstallRelocatorFactory,
	}
	command := newCommand(
		client,
		installDependencies,
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

type archiveFetcher interface {
	Fetch(context.Context, qtrepo.Archive) (string, error)
}

type archiveFetcherFactory func(string) (archiveFetcher, error)

type archiveExtractor interface {
	Extract(context.Context, string, string) error
}

type installRelocator interface {
	Relocate(context.Context, string) error
}

type installRelocatorFactory func(qtrepo.InstallPlan, string) (installRelocator, error)

type installCommandDependencies struct {
	resolver         installResolver
	fetcherFactory   archiveFetcherFactory
	extractor        archiveExtractor
	relocatorFactory installRelocatorFactory
}

type installExecutionMode uint8

const (
	installExecutionModeInstall installExecutionMode = iota + 1
	installExecutionModeDryRun
	installExecutionModeDownloadOnly
	installExecutionModeExtractOnly
)

func newCommand(
	lister versionLister,
	installDependencies installCommandDependencies,
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
			newListQtCommand(lister, defaultHost, output),
			newInstallQtCommand(
				installDependencies,
				defaultHost,
				output,
			),
		},
	}
}

func newListQtCommand(lister versionLister, defaultHost qtrepo.Host, output io.Writer) *cli.Command {
	return &cli.Command{
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
	}
}

func newInstallQtCommand(
	dependencies installCommandDependencies,
	defaultHost qtrepo.Host,
	output io.Writer,
) *cli.Command {
	return &cli.Command{
		Name:      "install-qt",
		Usage:     "Install or materialize a Qt SDK installation",
		ArgsUsage: "VERSION",
		Description: "Resolve and install native desktop Qt or Android Qt kits. " +
			"Android installation uses an existing matching desktop Qt. " +
			"The command can also stop after planning, downloading, or extraction.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "host",
				Value: string(defaultHost),
				Usage: "Host `platform`: windows, windows_arm64, mac, linux, or linux_arm64",
			},
			&cli.StringFlag{
				Name:     "target",
				Usage:    "Qt `target`: desktop or android",
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
			&cli.StringFlag{
				Name:  "cache-dir",
				Usage: "Archive cache root `DIR`; defaults to YAQT_CACHE_DIR or the operating system cache",
			},
			&cli.BoolFlag{
				Name:  "dry-run",
				Usage: "Print the installation plan without changing the filesystem",
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
			plan, err := dependencies.resolver.ResolveInstall(ctx, request)
			if err != nil {
				return err
			}
			if mode == installExecutionModeDryRun {
				return printInstallPlan(output, plan)
			}
			var relocator installRelocator
			if mode == installExecutionModeInstall {
				relocator, err = dependencies.relocatorFactory(plan, installRoot)
				if err != nil {
					return fmt.Errorf("configure Qt post-install relocation: %w", err)
				}
			}

			cacheDir, err := qtinstall.ResolveCacheDir(command.String("cache-dir"))
			if err != nil {
				return err
			}
			if dependencies.fetcherFactory == nil {
				return fmt.Errorf("archive downloader is not configured")
			}
			fetcher, err := dependencies.fetcherFactory(cacheDir)
			if err != nil {
				return fmt.Errorf("configure archive cache: %w", err)
			}
			if mode != installExecutionModeDownloadOnly && dependencies.extractor == nil {
				return fmt.Errorf("archive extractor is not configured")
			}
			return materializeInstallPlan(
				ctx,
				output,
				plan,
				cacheDir,
				fetcher,
				dependencies.extractor,
				relocator,
				mode,
			)
		},
	}
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

func printInstallPlan(output io.Writer, plan qtrepo.InstallPlan) error {
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
	for _, kit := range kits {
		if err := write("\n%s -> %s\n", kit.architecture, kit.destination); err != nil {
			return fmt.Errorf("write install plan: %w", err)
		}
		for _, packageSelection := range kit.packages {
			selection := "base package"
			if packageSelection.Module != "" {
				selection = "module " + packageSelection.Module
			}
			if err := write("  %s: %s\n", selection, packageSelection.Name); err != nil {
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
	return nil
}

func materializeInstallPlan(
	ctx context.Context,
	output io.Writer,
	plan qtrepo.InstallPlan,
	cacheDir string,
	fetcher archiveFetcher,
	extractor archiveExtractor,
	relocator installRelocator,
	mode installExecutionMode,
) error {
	if fetcher == nil {
		return fmt.Errorf("archive downloader is not configured")
	}
	switch mode {
	case installExecutionModeDownloadOnly:
	case installExecutionModeExtractOnly, installExecutionModeInstall:
		if extractor == nil {
			return fmt.Errorf("archive extractor is not configured")
		}
		if mode == installExecutionModeInstall && relocator == nil {
			return fmt.Errorf("Qt post-install relocator is not configured")
		}
	default:
		return fmt.Errorf("unsupported installation execution mode %d", mode)
	}

	if _, err := fmt.Fprintf(output, "Cache: %s\n", cacheDir); err != nil {
		return fmt.Errorf("write archive cache path: %w", err)
	}
	type cachedArchive struct {
		archive qtrepo.Archive
		path    string
	}
	cached := make([]cachedArchive, 0)
	kits, err := installPlanKits(plan)
	if err != nil {
		return err
	}
	for _, kit := range kits {
		for _, packageSelection := range kit.packages {
			for _, archive := range packageSelection.Archives {
				path, err := fetcher.Fetch(ctx, archive)
				if err != nil {
					return fmt.Errorf("cache archive %s: %w", archive.Name, err)
				}
				if _, err := fmt.Fprintf(output, "Cached %s: %s\n", archive.Name, path); err != nil {
					return fmt.Errorf("write cached archive path: %w", err)
				}
				cached = append(cached, cachedArchive{archive: archive, path: path})
			}
		}
	}
	if mode == installExecutionModeDownloadOnly {
		return nil
	}
	for _, item := range cached {
		if err := extractor.Extract(ctx, item.path, item.archive.ExtractTo); err != nil {
			return fmt.Errorf("extract archive %s: %w", item.archive.Name, err)
		}
		if _, err := fmt.Fprintf(
			output,
			"Extracted %s to %s\n",
			item.archive.Name,
			item.archive.ExtractTo,
		); err != nil {
			return fmt.Errorf("write extracted archive path: %w", err)
		}
	}
	if mode == installExecutionModeExtractOnly {
		if _, err := fmt.Fprintln(output, "Path relocation has not been applied."); err != nil {
			return fmt.Errorf("write extraction status: %w", err)
		}
		return nil
	}
	for _, kit := range kits {
		if err := relocator.Relocate(ctx, kit.destination); err != nil {
			return fmt.Errorf("relocate Qt kit %s: %w", kit.architecture, err)
		}
		if _, err := fmt.Fprintf(output, "Relocated %s kit: %s\n", kit.architecture, kit.destination); err != nil {
			return fmt.Errorf("write relocated Qt kit path: %w", err)
		}
	}
	if _, err := fmt.Fprintf(output, "Installed Qt %s for %s.\n", plan.Version, plan.Target); err != nil {
		return fmt.Errorf("write installation status: %w", err)
	}
	return nil
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
