package qtinstall

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/zengxs/yaqt/internal/qtrepo"
)

const desktopQtConfiguration = "[Paths]\nPrefix=..\n"

// DesktopRelocator makes an extracted desktop Qt installation independent of
// the paths used on Qt's build machines.
type DesktopRelocator struct {
	identity qtrepo.QtInstallationIdentity
	profile  hostProfile
}

// NewDesktopRelocator configures relocation for a native desktop Qt package.
func NewDesktopRelocator(identity qtrepo.QtInstallationIdentity) (*DesktopRelocator, error) {
	profile, err := hostProfileFor(identity.Host)
	if err != nil {
		return nil, err
	}
	if identity.Version.Major <= 0 || identity.Version.Minor < 0 || identity.Version.Patch < 0 {
		return nil, fmt.Errorf("invalid desktop Qt version %s", identity.Version)
	}
	return &DesktopRelocator{
		identity: identity,
		profile:  profile,
	}, nil
}

// Validate checks that kitDir is a matching native desktop Qt installation.
func (relocator *DesktopRelocator) Validate(ctx context.Context, kitDir string) error {
	_, err := relocator.validatedKitDirectory(ctx, kitDir)
	return err
}

// Relocate writes qt.conf and repairs text metadata that embeds Qt's build
// prefix. The extracted kit is validated before any file is changed.
func (relocator *DesktopRelocator) Relocate(
	ctx context.Context,
	kitDir string,
) (resultErr error) {
	absoluteKitDir, err := relocator.validatedKitDirectory(ctx, kitDir)
	if err != nil {
		return err
	}

	root, err := os.OpenRoot(absoluteKitDir)
	if err != nil {
		return fmt.Errorf("open desktop Qt kit directory %s: %w", absoluteKitDir, err)
	}
	defer func() {
		if err := root.Close(); err != nil && resultErr == nil {
			resultErr = fmt.Errorf("close desktop Qt kit directory %s: %w", absoluteKitDir, err)
		}
	}()

	updates := make([]relocationFileUpdate, 0)
	qtConfigurationUpdate, changed, err := prepareDesktopQtConfiguration(root)
	if err != nil {
		return fmt.Errorf("prepare desktop Qt configuration: %w", err)
	}
	if changed {
		updates = append(updates, qtConfigurationUpdate)
	}

	metadataGroups := []struct {
		directory string
		extension string
		transform func([]byte) ([]byte, error)
	}{
		{
			directory: filepath.Join("lib", "pkgconfig"),
			extension: ".pc",
			transform: func(contents []byte) ([]byte, error) {
				return replaceDesktopBuildPrefixes(contents, absoluteKitDir), nil
			},
		},
		{
			directory: "lib",
			extension: ".prl",
			transform: func(contents []byte) ([]byte, error) {
				return rewriteDesktopPRL(contents), nil
			},
		},
		{
			directory: "lib",
			extension: ".la",
			transform: func(contents []byte) ([]byte, error) {
				return replaceDesktopBuildPrefixes(contents, absoluteKitDir), nil
			},
		},
	}
	for _, group := range metadataGroups {
		paths, err := desktopMetadataFiles(root, group.directory, group.extension)
		if err != nil {
			return fmt.Errorf("inspect desktop Qt metadata: %w", err)
		}
		for _, path := range paths {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("relocate desktop Qt kit: %w", err)
			}
			update, changed, err := prepareRelocationFile(root, relocationFileSpec{
				path:      path,
				transform: group.transform,
			})
			if err != nil {
				return fmt.Errorf("prepare desktop Qt relocation for %s: %w", path, err)
			}
			if changed {
				updates = append(updates, update)
			}
		}
	}

	if err := applyRelocationFiles(ctx, root, updates); err != nil {
		return fmt.Errorf("apply desktop Qt relocation: %w", err)
	}
	return nil
}

func (relocator *DesktopRelocator) validatedKitDirectory(
	ctx context.Context,
	kitDir string,
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("relocate desktop Qt kit: %w", err)
	}
	if relocator == nil {
		return "", fmt.Errorf("desktop Qt relocator is not configured")
	}
	if strings.TrimSpace(kitDir) == "" {
		return "", fmt.Errorf("desktop Qt kit directory must not be empty")
	}
	if strings.ContainsAny(kitDir, "\r\n\x00") {
		return "", fmt.Errorf("desktop Qt kit directory contains unsupported characters")
	}

	absoluteKitDir, err := filepath.Abs(kitDir)
	if err != nil {
		return "", fmt.Errorf("resolve desktop Qt kit directory %s: %w", kitDir, err)
	}
	if err := validateHostQtDirectory(relocator.identity, absoluteKitDir, relocator.profile); err != nil {
		return "", fmt.Errorf("validate extracted desktop Qt kit %s: %w", absoluteKitDir, err)
	}
	return absoluteKitDir, nil
}

func prepareDesktopQtConfiguration(
	root *os.Root,
) (relocationFileUpdate, bool, error) {
	path := filepath.Join("bin", "qt.conf")
	if err := validateExistingParents(root, path); err != nil {
		return relocationFileUpdate{}, false, err
	}
	info, err := root.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return relocationFileUpdate{
			path:        path,
			contents:    []byte(desktopQtConfiguration),
			permissions: 0o644,
		}, true, nil
	}
	if err != nil {
		return relocationFileUpdate{}, false, fmt.Errorf("inspect %s: %w", path, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return relocationFileUpdate{}, false, fmt.Errorf("%s is a symbolic link", path)
	}
	if !info.Mode().IsRegular() {
		return relocationFileUpdate{}, false, fmt.Errorf("%s has unsupported mode %s", path, info.Mode())
	}
	if info.Size() > maximumRelocationFileSize {
		return relocationFileUpdate{}, false, fmt.Errorf(
			"%s is %d bytes; maximum supported size is %d bytes",
			path,
			info.Size(),
			maximumRelocationFileSize,
		)
	}
	contents, err := root.ReadFile(path)
	if err != nil {
		return relocationFileUpdate{}, false, fmt.Errorf("read %s: %w", path, err)
	}
	if bytes.Equal(contents, []byte(desktopQtConfiguration)) {
		return relocationFileUpdate{}, false, nil
	}
	return relocationFileUpdate{
		path:        path,
		contents:    []byte(desktopQtConfiguration),
		permissions: info.Mode().Perm(),
	}, true, nil
}

func desktopMetadataFiles(root *os.Root, directory, extension string) ([]string, error) {
	if err := validateExistingParents(root, filepath.Join(directory, "metadata"+extension)); err != nil {
		return nil, err
	}
	info, err := root.Lstat(directory)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect directory %s: %w", directory, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return nil, fmt.Errorf("metadata directory %s is a symbolic link", directory)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("metadata path %s is not a directory", directory)
	}

	handle, err := root.Open(directory)
	if err != nil {
		return nil, fmt.Errorf("open directory %s: %w", directory, err)
	}
	entries, readErr := handle.ReadDir(-1)
	closeErr := handle.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read directory %s: %w", directory, readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close directory %s: %w", directory, closeErr)
	}

	paths := make([]string, 0)
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != extension {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		entryInfo, err := root.Lstat(path)
		if err != nil {
			return nil, fmt.Errorf("inspect metadata file %s: %w", path, err)
		}
		if entryInfo.Mode()&fs.ModeSymlink != 0 {
			return nil, fmt.Errorf("metadata file %s is a symbolic link", path)
		}
		if !entryInfo.Mode().IsRegular() {
			return nil, fmt.Errorf("metadata file %s has unsupported mode %s", path, entryInfo.Mode())
		}
		paths = append(paths, path)
	}
	slices.Sort(paths)
	return paths, nil
}

func rewriteDesktopPRL(contents []byte) []byte {
	result := append([]byte(nil), contents...)
	for _, prefix := range desktopBuildPrefixes() {
		for _, libraryPath := range []string{
			prefix + "/lib",
			strings.ReplaceAll(prefix, "/", `\`) + `\lib`,
		} {
			result = bytes.ReplaceAll(result, []byte(libraryPath), []byte("$$[QT_INSTALL_LIBS]"))
		}
	}
	return result
}

func replaceDesktopBuildPrefixes(contents []byte, prefix string) []byte {
	if !utf8.Valid(contents) {
		return contents
	}
	result := append([]byte(nil), contents...)
	for _, buildPrefix := range desktopBuildPrefixes() {
		result = bytes.ReplaceAll(result, []byte(buildPrefix), []byte(prefix))
		windowsBuildPrefix := strings.ReplaceAll(buildPrefix, "/", `\`)
		windowsPrefix := strings.ReplaceAll(prefix, "/", `\`)
		result = bytes.ReplaceAll(result, []byte(windowsBuildPrefix), []byte(windowsPrefix))
	}
	return result
}

func desktopBuildPrefixes() []string {
	return []string{
		"/Users/qt/work/install",
		"/home/qt/work/install",
		"c:/Users/qt/work/install",
		"C:/Users/qt/work/install",
	}
}
