package main

import (
	"context"
	"fmt"
	"io"

	"github.com/urfave/cli/v3"

	"github.com/zengxs/yaqt/internal/qtrepo"
)

type moduleLister interface {
	ListModules(context.Context, qtrepo.ModuleRequest) ([]string, error)
}

func newListModulesCommand(
	lister moduleLister,
	defaultHost qtrepo.Host,
	output io.Writer,
) *cli.Command {
	return withRepositoryCache(&cli.Command{
		Name:      "list-modules",
		Usage:     "List additional Qt modules available for a package variant",
		ArgsUsage: "VERSION",
		Description: "Read a version and package variant's main and extension metadata and " +
			"print additional available modules in ascending order.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "host",
				Value: string(defaultHost),
				Usage: "Host `platform`: windows, windows_arm64, mac, linux, or linux_arm64",
			},
			&cli.StringFlag{
				Name:  "target",
				Value: string(qtrepo.TargetDesktop),
				Usage: "Qt `target`: desktop, android, or ios",
			},
			&cli.StringFlag{
				Name:  "arch",
				Usage: "Desktop Qt `architecture`; defaults to the native architecture for the selected host",
			},
			&cli.StringFlag{
				Name:     "abi",
				Usage:    "Android `ABI`: arm64-v8a, armeabi-v7a, x86, or x86_64",
				OnlyOnce: true,
			},
			&cli.StringFlag{
				Name:  "base-url",
				Value: qtrepo.DefaultBaseURL,
				Usage: "Qt download server or mirror `URL`",
			},
		},
		Action: func(ctx context.Context, command *cli.Command) error {
			if command.NArg() != 1 {
				return fmt.Errorf("list-modules requires exactly one Qt version")
			}
			version, err := qtrepo.ParseVersion(command.Args().First())
			if err != nil {
				return err
			}
			repository, err := repositoryFromCommand(command)
			if err != nil {
				return err
			}
			requestBuilder, err := moduleRequestBuilderFor(repository.Target)
			if err != nil {
				return err
			}
			request, err := requestBuilder.moduleRequest(command, repository, version)
			if err != nil {
				return err
			}
			if lister == nil {
				return fmt.Errorf("Qt module lister is not configured")
			}
			modules, err := lister.ListModules(ctx, request)
			if err != nil {
				return err
			}
			for _, module := range modules {
				if _, err := fmt.Fprintln(output, module); err != nil {
					return fmt.Errorf("write module list: %w", err)
				}
			}
			return nil
		},
	})
}
