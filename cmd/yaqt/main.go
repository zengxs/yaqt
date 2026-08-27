package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/zengxs/yaqt/internal/qtrepo"
)

func main() {
	defaultHost, err := qtrepo.CurrentHost()
	if err != nil {
		fmt.Fprintf(os.Stderr, "yaqt: %v\n", err)
		os.Exit(1)
	}

	client := qtrepo.NewClient(&http.Client{Timeout: 30 * time.Second})
	command := newCommand(client, defaultHost, os.Stdout, os.Stderr)
	if err := command.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "yaqt: %v\n", err)
		os.Exit(1)
	}
}

type versionLister interface {
	ListVersions(context.Context, qtrepo.Repository) ([]qtrepo.Version, error)
}

func newCommand(lister versionLister, defaultHost qtrepo.Host, output, errorOutput io.Writer) *cli.Command {
	return &cli.Command{
		Name:      "yaqt",
		Usage:     "Install Qt SDK components non-interactively",
		Version:   "0.1.0",
		Writer:    output,
		ErrWriter: errorOutput,
		Suggest:   true,
		Commands: []*cli.Command{
			{
				Name:        "list-qt",
				Usage:       "List supported Qt versions available in a repository",
				Description: "The command reads the selected repository's directory index and prints stable Qt 6.8.0 or newer releases, one per line in ascending order.",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "host",
						Value: string(defaultHost),
						Usage: "Repository `host`: windows, windows_arm64, mac, linux, linux_arm64, or all_os",
					},
					&cli.StringFlag{
						Name:  "target",
						Value: string(qtrepo.TargetDesktop),
						Usage: "Repository `target`: desktop, android, winrt, ios, wasm, or qt",
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

					host, err := qtrepo.ParseHost(command.String("host"))
					if err != nil {
						return err
					}
					target, err := qtrepo.ParseTarget(command.String("target"))
					if err != nil {
						return err
					}
					repository, err := qtrepo.NewRepository(command.String("base-url"), host, target)
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
			},
		},
	}
}
