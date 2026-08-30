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
	fetcherFactory := archiveFetcherFactory(func(cacheDir string) (archiveFetcher, error) {
		return qtinstall.NewArchiveStore(nil, cacheDir)
	})
	command := newCommand(
		client,
		client,
		fetcherFactory,
		qtinstall.SevenZipExtractor{},
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

type installExecutionMode uint8

const (
	installExecutionModeDryRun installExecutionMode = iota + 1
	installExecutionModeDownloadOnly
	installExecutionModeExtractOnly
)

func newCommand(
	lister versionLister,
	resolver installResolver,
	fetcherFactory archiveFetcherFactory,
	extractor archiveExtractor,
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
			newInstallQtCommand(resolver, fetcherFactory, extractor, defaultHost, output),
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
	resolver installResolver,
	fetcherFactory archiveFetcherFactory,
	extractor archiveExtractor,
	defaultHost qtrepo.Host,
	output io.Writer,
) *cli.Command {
	return &cli.Command{
		Name:      "install-qt",
		Usage:     "Plan, download, or extract a Qt SDK installation",
		ArgsUsage: "VERSION",
		Description: "Resolve the Qt archives, extraction paths, and host Qt requirement for an Android installation. " +
			"The command can print the plan, download and verify its archives, or extract them. " +
			"Android path relocation is not yet available.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "host",
				Value: string(defaultHost),
				Usage: "Host `platform`: windows, windows_arm64, mac, linux, or linux_arm64",
			},
			&cli.StringFlag{
				Name:     "target",
				Usage:    "Qt `target`; currently android",
				Required: true,
			},
			&cli.StringSliceFlag{
				Name:     "abi",
				Usage:    "Android `ABI`: arm64-v8a, armeabi-v7a, x86, or x86_64; may be repeated",
				Required: true,
			},
			&cli.StringSliceFlag{
				Name:    "module",
				Aliases: []string{"m"},
				Usage:   "Additional Qt `module`; may be repeated",
			},
			&cli.StringFlag{
				Name:  "output-dir",
				Value: ".",
				Usage: "Installation root `DIR`",
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
			repository, err := repositoryFromCommand(command)
			if err != nil {
				return err
			}
			if repository.Target != qtrepo.TargetAndroid {
				return fmt.Errorf("install-qt currently supports only the Android target")
			}

			abiValues := command.StringSlice("abi")
			abis := make([]qtrepo.AndroidABI, 0, len(abiValues))
			for _, value := range abiValues {
				abi, err := qtrepo.ParseAndroidABI(value)
				if err != nil {
					return err
				}
				abis = append(abis, abi)
			}
			plan, err := resolver.ResolveInstall(ctx, qtrepo.InstallRequest{
				Repository:  repository,
				Version:     version,
				AndroidABIs: abis,
				Modules:     command.StringSlice("module"),
				Destination: command.String("output-dir"),
			})
			if err != nil {
				return err
			}
			if mode == installExecutionModeDryRun {
				return printInstallPlan(output, plan)
			}

			cacheDir, err := qtinstall.ResolveCacheDir(command.String("cache-dir"))
			if err != nil {
				return err
			}
			if fetcherFactory == nil {
				return fmt.Errorf("archive downloader is not configured")
			}
			fetcher, err := fetcherFactory(cacheDir)
			if err != nil {
				return fmt.Errorf("configure archive cache: %w", err)
			}
			if mode == installExecutionModeExtractOnly && extractor == nil {
				return fmt.Errorf("archive extractor is not configured")
			}
			return materializeInstallPlan(ctx, output, plan, cacheDir, fetcher, extractor, mode)
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
		return 0, fmt.Errorf(
			"installation is not implemented yet; use --dry-run, --download-only, or --extract-only",
		)
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

	if err := write("Qt %s for %s\n", plan.Version, plan.Target); err != nil {
		return fmt.Errorf("write install plan: %w", err)
	}
	if err := write(
		"Host Qt requirement: %s desktop %s\n",
		plan.HostQt.Host,
		plan.HostQt.Version,
	); err != nil {
		return fmt.Errorf("write install plan: %w", err)
	}
	for _, kit := range plan.AndroidKits {
		if err := write("\n%s -> %s\n", kit.ABI, kit.Destination); err != nil {
			return fmt.Errorf("write install plan: %w", err)
		}
		for _, packageSelection := range kit.Packages {
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
	if err := write("\nPost-install: relocate Android Qt paths and connect the kit to the matching host Qt.\n"); err != nil {
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
	mode installExecutionMode,
) error {
	if _, err := fmt.Fprintf(output, "Cache: %s\n", cacheDir); err != nil {
		return fmt.Errorf("write archive cache path: %w", err)
	}
	type cachedArchive struct {
		archive qtrepo.Archive
		path    string
	}
	cached := make([]cachedArchive, 0)
	for _, kit := range plan.AndroidKits {
		for _, packageSelection := range kit.Packages {
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
	if mode != installExecutionModeExtractOnly {
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
	if _, err := fmt.Fprintln(output, "Path relocation has not been applied."); err != nil {
		return fmt.Errorf("write extraction status: %w", err)
	}
	return nil
}
