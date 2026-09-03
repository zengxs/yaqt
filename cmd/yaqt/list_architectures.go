package main

import (
	"context"
	"fmt"
	"io"

	"github.com/urfave/cli/v3"

	"github.com/zengxs/yaqt/internal/qtrepo"
)

type architectureLister interface {
	ListArchitectures(context.Context, qtrepo.ArchitectureRequest) ([]string, error)
}

func newListArchitecturesCommand(
	lister architectureLister,
	defaultHost qtrepo.Host,
	output io.Writer,
) *cli.Command {
	return withRepositoryCache(&cli.Command{
		Name:      "list-architectures",
		Usage:     "List Qt package architectures available for a release",
		ArgsUsage: "VERSION",
		Description: "Read a release's package variant repositories and print " +
			"available architecture identifiers in ascending order.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "host",
				Value: string(defaultHost),
				Usage: "Host `platform`: windows, windows_arm64, mac, linux, or linux_arm64",
			},
			&cli.StringFlag{
				Name:  "target",
				Value: string(qtrepo.TargetDesktop),
				Usage: "Qt `target`: desktop, android, ios, or wasm",
			},
			&cli.StringFlag{
				Name:  "base-url",
				Value: qtrepo.DefaultBaseURL,
				Usage: "Qt download server or mirror `URL`",
			},
		},
		Action: func(ctx context.Context, command *cli.Command) error {
			if command.NArg() != 1 {
				return fmt.Errorf("list-architectures requires exactly one Qt version")
			}
			version, err := qtrepo.ParseVersion(command.Args().First())
			if err != nil {
				return err
			}
			repository, err := repositoryFromCommand(command)
			if err != nil {
				return err
			}
			if lister == nil {
				return fmt.Errorf("Qt architecture lister is not configured")
			}
			architectures, err := lister.ListArchitectures(ctx, qtrepo.ArchitectureRequest{
				Repository: repository,
				Version:    version,
			})
			if err != nil {
				return err
			}
			for _, architecture := range architectures {
				if _, err := fmt.Fprintln(output, architecture); err != nil {
					return fmt.Errorf("write architecture list: %w", err)
				}
			}
			return nil
		},
	})
}
