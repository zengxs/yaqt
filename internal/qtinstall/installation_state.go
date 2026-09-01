package qtinstall

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/zengxs/yaqt/internal/qtrepo"
)

const (
	manifestSchemaVersion = 1
	maximumManifestSize   = 1 << 20
)

// PackageAction describes how an installation reconciles one repository package.
type PackageAction string

const (
	PackageActionInstall PackageAction = "install"
	PackageActionUpdate  PackageAction = "update"
	PackageActionSkip    PackageAction = "skip"
	PackageActionAdopt   PackageAction = "adopt"
)

// InstallationIdentity identifies the Qt installation shared by one or more kits.
type InstallationIdentity struct {
	Version qtrepo.Version
	Host    qtrepo.Host
	Target  qtrepo.Target
}

// KitRequest describes the desired repository packages for one Qt kit.
type KitRequest struct {
	Architecture string
	Destination  string
	Packages     []qtrepo.PackageSelection
}

// ReconciledPackage pairs a selected repository package with its local action.
type ReconciledPackage struct {
	Selection qtrepo.PackageSelection
	Action    PackageAction
}

// ReconciledKit contains the local actions for one desired Qt kit.
type ReconciledKit struct {
	Architecture string
	Destination  string
	Packages     []ReconciledPackage
	root         string
	existing     bool
	manifest     kitManifest
}

func (kit ReconciledKit) requiresValidation() bool {
	return kit.existing
}

func (kit ReconciledKit) hasChanges() bool {
	for _, selected := range kit.Packages {
		if selected.Action != PackageActionSkip {
			return true
		}
	}
	return false
}

func (kit ReconciledKit) pendingPackages() []qtrepo.PackageSelection {
	packages := make([]qtrepo.PackageSelection, 0, len(kit.Packages))
	for _, selected := range kit.Packages {
		if selected.Action == PackageActionInstall || selected.Action == PackageActionUpdate {
			packages = append(packages, selected.Selection)
		}
	}
	return packages
}

func (kit ReconciledKit) commit() error {
	if !kit.hasChanges() {
		return nil
	}
	return writeKitManifest(kit.root, kit.Destination, kit.manifest)
}

// Reconciliation contains the local actions for all requested Qt kits.
type Reconciliation struct {
	Kits []ReconciledKit
}

// IsSatisfied reports whether every selected package is already installed.
func (reconciliation Reconciliation) IsSatisfied() bool {
	for _, kit := range reconciliation.Kits {
		if kit.hasChanges() {
			return false
		}
	}
	return true
}

type kitManifest struct {
	SchemaVersion int                        `json:"schema_version"`
	QtVersion     string                     `json:"qt_version"`
	Host          qtrepo.Host                `json:"host"`
	Target        qtrepo.Target              `json:"target"`
	Architecture  string                     `json:"architecture"`
	Packages      map[string]manifestPackage `json:"packages"`
}

type manifestPackage struct {
	Version  string            `json:"version"`
	Module   string            `json:"module,omitempty"`
	Archives []manifestArchive `json:"archives"`
}

type manifestArchive struct {
	Name              string                   `json:"name"`
	URL               string                   `json:"url"`
	ChecksumAlgorithm qtrepo.ChecksumAlgorithm `json:"checksum_algorithm"`
	ChecksumURL       string                   `json:"checksum_url"`
}

// ReconcileInstallation compares desired repository packages with each kit's
// manifest without changing the installation.
func ReconcileInstallation(
	identity InstallationIdentity,
	installationRoot string,
	kits []KitRequest,
) (Reconciliation, error) {
	rootPath, err := cleanManagedRootPath(installationRoot)
	if err != nil {
		return Reconciliation{}, err
	}
	managedRoot, _, err := openManagedPathRoot(rootPath, false)
	if err != nil {
		return Reconciliation{}, fmt.Errorf("open Qt installation root %s: %w", rootPath, err)
	}
	if managedRoot != nil {
		defer func() { _ = managedRoot.close() }()
	}
	result := Reconciliation{Kits: make([]ReconciledKit, 0, len(kits))}
	for _, requestedKit := range kits {
		kit, err := reconcileKit(identity, rootPath, managedRoot, requestedKit)
		if err != nil {
			return Reconciliation{}, err
		}
		result.Kits = append(result.Kits, kit)
	}
	return result, nil
}

func reconcileKit(
	identity InstallationIdentity,
	installationRoot string,
	managedRoot *managedPathRoot,
	request KitRequest,
) (ReconciledKit, error) {
	destination, err := filepath.Abs(request.Destination)
	if err != nil {
		return ReconciledKit{}, fmt.Errorf("resolve Qt kit path %s: %w", request.Destination, err)
	}
	destination = filepath.Clean(destination)
	if _, err := relativeManagedPath(installationRoot, destination); err != nil {
		return ReconciledKit{}, err
	}
	manifest, found, err := readKitManifest(managedRoot, destination)
	if err != nil {
		return ReconciledKit{}, fmt.Errorf("read Qt kit state %s: %w", destination, err)
	}
	existing := found
	if !found && managedRoot != nil {
		existing, err = managedRoot.inspectResolvedDirectory(destination, "Qt kit path")
		if err != nil {
			return ReconciledKit{}, fmt.Errorf("inspect Qt kit %s: %w", destination, err)
		}
	}
	if found {
		if err := validateManifestIdentity(manifest, identity, request.Architecture); err != nil {
			return ReconciledKit{}, fmt.Errorf("validate Qt kit state %s: %w", destination, err)
		}
	} else {
		manifest = kitManifest{
			SchemaVersion: manifestSchemaVersion,
			QtVersion:     identity.Version.String(),
			Host:          identity.Host,
			Target:        identity.Target,
			Architecture:  request.Architecture,
			Packages:      make(map[string]manifestPackage),
		}
	}

	result := ReconciledKit{
		Architecture: request.Architecture,
		Destination:  destination,
		Packages:     make([]ReconciledPackage, 0, len(request.Packages)),
		root:         installationRoot,
		existing:     existing,
		manifest:     manifest,
	}
	seen := make(map[string]struct{}, len(request.Packages))
	for _, selection := range request.Packages {
		name := strings.TrimSpace(selection.Name)
		packageVersion := strings.TrimSpace(selection.PackageVersion)
		if name == "" {
			return ReconciledKit{}, fmt.Errorf("Qt kit %s contains a package without a name", destination)
		}
		if packageVersion == "" {
			return ReconciledKit{}, fmt.Errorf("Qt package %q contains no repository version", name)
		}
		if _, duplicate := seen[name]; duplicate {
			return ReconciledKit{}, fmt.Errorf("Qt kit %s selects package %q more than once", destination, name)
		}
		seen[name] = struct{}{}

		action := PackageActionInstall
		if !found && existing && selection.Module == "" {
			action = PackageActionAdopt
		} else if installed, ok := manifest.Packages[name]; ok {
			action = PackageActionUpdate
			if installed.Version == packageVersion && installed.Module == selection.Module {
				action = PackageActionSkip
			}
		}
		result.Packages = append(result.Packages, ReconciledPackage{
			Selection: selection,
			Action:    action,
		})
		replacePackageRecord(manifest.Packages, selection)
	}
	result.manifest = manifest
	return result, nil
}

func replacePackageRecord(
	packages map[string]manifestPackage,
	selection qtrepo.PackageSelection,
) {
	for name, installed := range packages {
		if name == selection.Name {
			continue
		}
		if selection.Module == "" && installed.Module == "" ||
			selection.Module != "" && installed.Module == selection.Module {
			delete(packages, name)
		}
	}
	archives := make([]manifestArchive, 0, len(selection.Archives))
	for _, archive := range selection.Archives {
		archives = append(archives, manifestArchive{
			Name:              archive.Name,
			URL:               archive.URL,
			ChecksumAlgorithm: archive.Checksum.Algorithm,
			ChecksumURL:       archive.Checksum.URL,
		})
	}
	packages[selection.Name] = manifestPackage{
		Version:  selection.PackageVersion,
		Module:   selection.Module,
		Archives: archives,
	}
}

func validateManifestIdentity(
	manifest kitManifest,
	identity InstallationIdentity,
	architecture string,
) error {
	if manifest.SchemaVersion != manifestSchemaVersion {
		return fmt.Errorf(
			"unsupported manifest schema %d; expected %d",
			manifest.SchemaVersion,
			manifestSchemaVersion,
		)
	}
	if manifest.QtVersion != identity.Version.String() || manifest.Host != identity.Host ||
		manifest.Target != identity.Target || manifest.Architecture != architecture {
		return fmt.Errorf(
			"manifest identifies Qt %s for %s/%s architecture %q, not Qt %s for %s/%s architecture %q",
			manifest.QtVersion,
			manifest.Host,
			manifest.Target,
			manifest.Architecture,
			identity.Version,
			identity.Host,
			identity.Target,
			architecture,
		)
	}
	if manifest.Packages == nil {
		return fmt.Errorf("manifest contains no package map")
	}
	for name, installed := range manifest.Packages {
		if strings.TrimSpace(name) == "" || strings.TrimSpace(installed.Version) == "" {
			return fmt.Errorf("manifest contains an invalid package record")
		}
	}
	return nil
}

func readKitManifest(
	root *managedPathRoot,
	kitDirectory string,
) (kitManifest, bool, error) {
	if root == nil {
		return kitManifest{}, false, nil
	}
	stateDirectory := filepath.Join(kitDirectory, ".yaqt")
	found, err := root.inspectDirectory(stateDirectory, "Qt kit state directory")
	if err != nil || !found {
		return kitManifest{}, false, err
	}
	path := filepath.Join(stateDirectory, "manifest.json")
	found, err = root.inspectFile(path, "Qt kit manifest")
	if err != nil || !found {
		return kitManifest{}, false, err
	}
	file, err := root.openFile(path, os.O_RDONLY, 0)
	if err != nil {
		return kitManifest{}, false, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return kitManifest{}, false, err
	}
	if !info.Mode().IsRegular() {
		return kitManifest{}, false, fmt.Errorf("manifest is not a regular file")
	}
	if info.Size() > maximumManifestSize {
		return kitManifest{}, false, fmt.Errorf(
			"manifest is %d bytes; maximum supported size is %d bytes",
			info.Size(),
			maximumManifestSize,
		)
	}

	decoder := json.NewDecoder(io.LimitReader(file, maximumManifestSize+1))
	decoder.DisallowUnknownFields()
	var manifest kitManifest
	if err := decoder.Decode(&manifest); err != nil {
		return kitManifest{}, false, fmt.Errorf("decode manifest: %w", err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return kitManifest{}, false, err
	}
	return manifest, true, nil
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode trailing manifest data: %w", err)
	}
	return fmt.Errorf("manifest contains multiple JSON values")
}

func writeKitManifest(
	installationRoot string,
	kitDirectory string,
	manifest kitManifest,
) (resultErr error) {
	root, _, err := openManagedPathRoot(installationRoot, true)
	if err != nil {
		return fmt.Errorf("open Qt installation root %s: %w", installationRoot, err)
	}
	defer func() {
		if err := root.close(); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("close Qt installation root: %w", err))
		}
	}()
	stateDirectory := filepath.Join(kitDirectory, ".yaqt")
	if err := root.ensureDirectory(stateDirectory, "Qt kit state directory"); err != nil {
		return fmt.Errorf("create Qt kit state directory %s: %w", stateDirectory, err)
	}
	temporary, temporaryPath, err := root.createTemporaryFile(stateDirectory, ".manifest-")
	if err != nil {
		return fmt.Errorf("create staged Qt kit manifest: %w", err)
	}
	defer func() {
		if temporary != nil {
			if err := temporary.Close(); err != nil && resultErr == nil {
				resultErr = fmt.Errorf("close staged Qt kit manifest: %w", err)
			}
		}
		if temporaryPath != "" {
			_ = root.remove(temporaryPath)
		}
	}()

	if err := temporary.Chmod(0o644); err != nil {
		return fmt.Errorf("set staged Qt kit manifest permissions: %w", err)
	}
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(manifest); err != nil {
		return fmt.Errorf("write staged Qt kit manifest: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync staged Qt kit manifest: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close staged Qt kit manifest: %w", err)
	}
	temporary = nil

	manifestPath := filepath.Join(stateDirectory, "manifest.json")
	if err := root.rename(temporaryPath, manifestPath); err != nil {
		return fmt.Errorf("publish Qt kit manifest %s: %w", manifestPath, err)
	}
	temporaryPath = ""
	return nil
}
