package qtinstall

import (
	"context"
	"fmt"
	"io"

	"github.com/zengxs/yaqt/internal/cache"
	"github.com/zengxs/yaqt/internal/qtrepo"
)

// ArchiveFetcher retrieves and verifies one repository archive.
type ArchiveFetcher interface {
	Fetch(context.Context, qtrepo.Archive) (string, error)
}

// ArchiveFetcherFactory configures the archive cache used by an installation.
type ArchiveFetcherFactory func(string) (ArchiveFetcher, error)

// ArchiveExtractor extracts one verified archive into its planned destination.
type ArchiveExtractor interface {
	Extract(context.Context, string, string) error
}

// Relocator validates and rewrites one extracted Qt kit.
type Relocator interface {
	Validate(context.Context, string) error
	Relocate(context.Context, string) error
}

// RelocatorFactory configures target-specific relocation while the version is locked.
type RelocatorFactory func() (Relocator, error)

// ExecutionMode selects how far an installation request is materialized.
type ExecutionMode uint8

const (
	ExecutionModeInstall ExecutionMode = iota + 1
	ExecutionModeDownloadOnly
	ExecutionModeExtractOnly
)

// MaterializeRequest describes one resolved installation to execute.
type MaterializeRequest struct {
	Identity InstallationIdentity
	Root     string
	CacheDir string
	Kits     []KitRequest
	Mode     ExecutionMode
}

// Installer materializes resolved repository packages while keeping local
// state, locking, and publication order behind one interface.
type Installer struct {
	fetcherFactory   ArchiveFetcherFactory
	extractor        ArchiveExtractor
	relocatorFactory RelocatorFactory
}

// NewInstaller configures an installation materializer.
func NewInstaller(
	fetcherFactory ArchiveFetcherFactory,
	extractor ArchiveExtractor,
	relocatorFactory RelocatorFactory,
) *Installer {
	return &Installer{
		fetcherFactory:   fetcherFactory,
		extractor:        extractor,
		relocatorFactory: relocatorFactory,
	}
}

// Materialize executes a resolved installation request. Complete installation
// state is committed only after successful relocation.
func (installer *Installer) Materialize(
	ctx context.Context,
	output io.Writer,
	request MaterializeRequest,
) error {
	if output == nil {
		output = io.Discard
	}
	operation := func() error {
		return installer.materializeLocked(ctx, output, request)
	}
	switch request.Mode {
	case ExecutionModeInstall, ExecutionModeExtractOnly:
		return withVersionLock(
			ctx,
			request.Root,
			request.Identity.Version,
			func() error {
				return writeInstallOutput(
					output,
					"Waiting for another installation of Qt %s to finish.\n",
					request.Identity.Version,
				)
			},
			operation,
		)
	case ExecutionModeDownloadOnly:
		return operation()
	default:
		return fmt.Errorf("unsupported installation execution mode %d", request.Mode)
	}
}

func (installer *Installer) materializeLocked(
	ctx context.Context,
	output io.Writer,
	request MaterializeRequest,
) error {
	if installer == nil {
		return fmt.Errorf("Qt installer is not configured")
	}

	selectedPackages := make([][]qtrepo.PackageSelection, len(request.Kits))
	var reconciliation Reconciliation
	var relocator Relocator
	if request.Mode == ExecutionModeInstall {
		var err error
		reconciliation, err = ReconcileInstallation(request.Identity, request.Root, request.Kits)
		if err != nil {
			return fmt.Errorf("reconcile Qt installation: %w", err)
		}
		if installer.relocatorFactory == nil {
			return fmt.Errorf("Qt post-install relocator is not configured")
		}
		relocator, err = installer.relocatorFactory()
		if err != nil {
			return fmt.Errorf("configure Qt post-install relocation: %w", err)
		}
		for kitIndex, kit := range reconciliation.Kits {
			if kit.requiresValidation() {
				if err := relocator.Validate(ctx, kit.Destination); err != nil {
					return fmt.Errorf("validate existing Qt kit %s: %w", kit.Architecture, err)
				}
			}
			selectedPackages[kitIndex] = kit.pendingPackages()
		}
		if reconciliation.IsSatisfied() {
			return writeInstallOutput(
				output,
				"Qt %s for %s is already satisfied.\n",
				request.Identity.Version,
				request.Identity.Target,
			)
		}
	} else {
		for kitIndex, kit := range request.Kits {
			selectedPackages[kitIndex] = kit.Packages
		}
	}

	type cachedArchive struct {
		archive qtrepo.Archive
		path    string
	}
	cached := make([]cachedArchive, 0)
	for _, packages := range selectedPackages {
		for _, packageSelection := range packages {
			for _, archive := range packageSelection.Archives {
				cached = append(cached, cachedArchive{archive: archive})
			}
		}
	}
	if len(cached) > 0 {
		if installer.fetcherFactory == nil {
			return fmt.Errorf("archive downloader is not configured")
		}
		cacheDir, err := cache.ResolveRoot(request.CacheDir)
		if err != nil {
			return err
		}
		fetcher, err := installer.fetcherFactory(cacheDir)
		if err != nil {
			return fmt.Errorf("configure archive cache: %w", err)
		}
		if err := writeInstallOutput(output, "Cache: %s\n", cacheDir); err != nil {
			return err
		}
		for index := range cached {
			path, err := fetcher.Fetch(ctx, cached[index].archive)
			if err != nil {
				return fmt.Errorf("cache archive %s: %w", cached[index].archive.Name, err)
			}
			cached[index].path = path
			if err := writeInstallOutput(
				output,
				"Cached %s: %s\n",
				cached[index].archive.Name,
				path,
			); err != nil {
				return err
			}
		}
	}
	if request.Mode == ExecutionModeDownloadOnly {
		return nil
	}
	if len(cached) > 0 && installer.extractor == nil {
		return fmt.Errorf("archive extractor is not configured")
	}
	for _, item := range cached {
		if err := installer.extractor.Extract(ctx, item.path, item.archive.ExtractTo); err != nil {
			return fmt.Errorf("extract archive %s: %w", item.archive.Name, err)
		}
		if err := writeInstallOutput(
			output,
			"Extracted %s to %s\n",
			item.archive.Name,
			item.archive.ExtractTo,
		); err != nil {
			return err
		}
	}
	if request.Mode == ExecutionModeExtractOnly {
		return writeInstallOutput(output, "Path relocation has not been applied.\n")
	}

	for kitIndex, kit := range request.Kits {
		if !reconciliation.Kits[kitIndex].hasChanges() {
			continue
		}
		if err := relocator.Relocate(ctx, kit.Destination); err != nil {
			return fmt.Errorf("relocate Qt kit %s: %w", kit.Architecture, err)
		}
		if err := writeInstallOutput(
			output,
			"Relocated %s kit: %s\n",
			kit.Architecture,
			kit.Destination,
		); err != nil {
			return err
		}
	}
	for kitIndex, kit := range request.Kits {
		if !reconciliation.Kits[kitIndex].hasChanges() {
			continue
		}
		if err := reconciliation.Kits[kitIndex].commit(); err != nil {
			return fmt.Errorf("record installed Qt kit %s: %w", kit.Architecture, err)
		}
	}
	return writeInstallOutput(
		output,
		"Installed Qt %s for %s.\n",
		request.Identity.Version,
		request.Identity.Target,
	)
}

func writeInstallOutput(output io.Writer, format string, arguments ...any) error {
	if _, err := fmt.Fprintf(output, format, arguments...); err != nil {
		return fmt.Errorf("write installation status: %w", err)
	}
	return nil
}
