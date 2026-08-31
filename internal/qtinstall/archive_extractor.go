package qtinstall

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/bodgit/sevenzip"
)

const (
	defaultDirectoryMode = 0o755
	defaultFileMode      = 0o644
	maxSymlinkDepth      = 255
	maxSymlinkTargetSize = 4096
	safePermissionMask   = 0o755
)

// SevenZipExtractor safely extracts regular files, directories, and relative
// symbolic links from a 7-Zip archive. It rejects entries that could escape or
// redirect writes from the destination root.
type SevenZipExtractor struct{}

type extractionEntry struct {
	file         *sevenzip.File
	relativePath string
	mode         fs.FileMode
	linkTarget   string
}

// Extract extracts archivePath into destination. All archive entries and
// existing destination conflicts are validated before the first file is
// written.
func (SevenZipExtractor) Extract(
	ctx context.Context,
	archivePath string,
	destination string,
) (resultErr error) {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("extract 7-Zip archive: %w", err)
	}
	if archivePath == "" {
		return fmt.Errorf("7-Zip archive path must not be empty")
	}
	if destination == "" {
		return fmt.Errorf("extraction destination must not be empty")
	}

	archive, err := sevenzip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open 7-Zip archive %s: %w", archivePath, err)
	}
	defer func() {
		if err := archive.Close(); err != nil && resultErr == nil {
			resultErr = fmt.Errorf("close 7-Zip archive %s: %w", archivePath, err)
		}
	}()

	entries, err := preflightArchive(ctx, archive.File)
	if err != nil {
		return fmt.Errorf("validate 7-Zip archive %s: %w", archivePath, err)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("extract 7-Zip archive %s: %w", archivePath, err)
	}

	destinationPath, err := filepath.Abs(destination)
	if err != nil {
		return fmt.Errorf("resolve extraction destination %s: %w", destination, err)
	}
	if err := os.MkdirAll(destinationPath, defaultDirectoryMode); err != nil {
		return fmt.Errorf("create extraction destination %s: %w", destinationPath, err)
	}
	root, err := os.OpenRoot(destinationPath)
	if err != nil {
		return fmt.Errorf("open extraction destination %s: %w", destinationPath, err)
	}
	defer func() {
		if err := root.Close(); err != nil && resultErr == nil {
			resultErr = fmt.Errorf("close extraction destination %s: %w", destinationPath, err)
		}
	}()

	if err := preflightDestination(root, entries); err != nil {
		return fmt.Errorf("validate extraction destination %s: %w", destinationPath, err)
	}

	directories := make([]extractionEntry, 0)
	symlinks := make([]extractionEntry, 0)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("extract 7-Zip archive %s: %w", archivePath, err)
		}
		if entry.mode.IsDir() {
			if err := root.MkdirAll(entry.relativePath, defaultDirectoryMode); err != nil {
				return fmt.Errorf("create archive directory %q: %w", entry.file.Name, err)
			}
			directories = append(directories, entry)
			continue
		}
		if isSymlink(entry.mode) {
			symlinks = append(symlinks, entry)
			continue
		}
		if err := extractRegularFile(ctx, root, entry); err != nil {
			return fmt.Errorf("extract archive entry %q: %w", entry.file.Name, err)
		}
	}

	for _, entry := range symlinks {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("extract 7-Zip archive %s: %w", archivePath, err)
		}
		parent := filepath.Dir(entry.relativePath)
		if parent != "." {
			if err := root.MkdirAll(parent, defaultDirectoryMode); err != nil {
				return fmt.Errorf("create archive symbolic link parent %q: %w", entry.file.Name, err)
			}
		}
	}

	separator := string(filepath.Separator)
	slices.SortFunc(directories, func(left, right extractionEntry) int {
		return strings.Count(right.relativePath, separator) - strings.Count(left.relativePath, separator)
	})
	for _, entry := range directories {
		if err := root.Chmod(entry.relativePath, archivePermissions(entry.mode)); err != nil {
			return fmt.Errorf("set archive directory mode %q: %w", entry.file.Name, err)
		}
	}

	for _, entry := range symlinks {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("extract 7-Zip archive %s: %w", archivePath, err)
		}
		if err := extractSymlink(root, entry); err != nil {
			return fmt.Errorf("extract archive symbolic link %q: %w", entry.file.Name, err)
		}
	}
	return nil
}

func preflightArchive(ctx context.Context, files []*sevenzip.File) ([]extractionEntry, error) {
	entries := make([]extractionEntry, 0, len(files))
	seen := make(map[string]struct{}, len(files))
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		mode := file.Mode()
		relativePath, err := validateArchiveEntry(file.Name, mode)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[relativePath]; exists {
			return nil, fmt.Errorf("archive contains duplicate entry %q", file.Name)
		}
		seen[relativePath] = struct{}{}
		linkTarget := ""
		if isSymlink(mode) {
			linkTarget, err = readSymlinkTarget(ctx, file)
			if err != nil {
				return nil, fmt.Errorf("archive symbolic link %q: %w", file.Name, err)
			}
		}
		entries = append(entries, extractionEntry{
			file:         file,
			relativePath: relativePath,
			mode:         mode,
			linkTarget:   linkTarget,
		})
	}
	if err := validateArchiveLayout(entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func validateArchiveLayout(entries []extractionEntry) error {
	modes := make(map[string]fs.FileMode, len(entries))
	for _, entry := range entries {
		modes[entry.relativePath] = entry.mode
	}
	for _, entry := range entries {
		for parent := filepath.Dir(entry.relativePath); parent != "."; parent = filepath.Dir(parent) {
			if mode, exists := modes[parent]; exists && !mode.IsDir() {
				return fmt.Errorf(
					"archive entry %q has non-directory parent %q",
					entry.relativePath,
					parent,
				)
			}
		}
	}
	return validateSymlinkTargets(nil, entries)
}

func validateArchiveEntry(name string, mode fs.FileMode) (string, error) {
	if mode.IsDir() {
		name = strings.TrimSuffix(name, "/")
	}
	if name == "" || name == "." || !fs.ValidPath(name) ||
		strings.ContainsAny(name, "\\:\x00") {
		return "", fmt.Errorf("archive contains unsafe entry path %q", name)
	}
	if !mode.IsDir() && !mode.IsRegular() && !isSymlink(mode) {
		return "", fmt.Errorf("archive entry %q has unsupported type %s", name, mode.Type())
	}

	relativePath := filepath.FromSlash(name)
	if filepath.IsAbs(relativePath) || filepath.VolumeName(relativePath) != "" {
		return "", fmt.Errorf("archive contains unsafe entry path %q", name)
	}
	return relativePath, nil
}

func readSymlinkTarget(ctx context.Context, file *sevenzip.File) (string, error) {
	if file.UncompressedSize == 0 {
		return "", fmt.Errorf("target must not be empty")
	}
	if file.UncompressedSize > maxSymlinkTargetSize {
		return "", fmt.Errorf(
			"target is %d bytes, exceeds the %d-byte limit",
			file.UncompressedSize,
			maxSymlinkTargetSize,
		)
	}

	source, err := file.Open()
	if err != nil {
		return "", fmt.Errorf("open compressed target: %w", err)
	}
	contents, readErr := io.ReadAll(contextReader{ctx: ctx, reader: source})
	closeErr := source.Close()
	if readErr != nil {
		return "", fmt.Errorf("read compressed target: %w", readErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("close compressed target: %w", closeErr)
	}
	if uint64(len(contents)) != file.UncompressedSize {
		return "", fmt.Errorf(
			"target is %d bytes, expected %d",
			len(contents),
			file.UncompressedSize,
		)
	}
	return validateSymlinkTarget(file.Name, string(contents))
}

func validateSymlinkTarget(entryName, target string) (string, error) {
	if err := resolveSymlinkTarget(entryName, target, nil); err != nil {
		return "", err
	}
	return filepath.FromSlash(target), nil
}

type symlinkTargetLookup func(string) (string, bool, error)

func validateSymlinkTargets(root *os.Root, entries []extractionEntry) error {
	archiveEntries := make(map[string]extractionEntry, len(entries))
	for _, entry := range entries {
		archiveEntries[filepath.ToSlash(entry.relativePath)] = entry
	}

	lookup := func(relativePath string) (string, bool, error) {
		if entry, ok := archiveEntries[relativePath]; ok {
			if isSymlink(entry.mode) {
				return filepath.ToSlash(entry.linkTarget), true, nil
			}
			return "", false, nil
		}
		if root == nil {
			return "", false, nil
		}

		path := filepath.FromSlash(relativePath)
		info, err := root.Lstat(path)
		if errors.Is(err, fs.ErrNotExist) {
			return "", false, nil
		}
		if err != nil {
			return "", false, fmt.Errorf("inspect symbolic link target component %q: %w", relativePath, err)
		}
		if info.Mode()&fs.ModeSymlink == 0 {
			return "", false, nil
		}
		target, err := root.Readlink(path)
		if err != nil {
			return "", false, fmt.Errorf("read symbolic link target component %q: %w", relativePath, err)
		}
		return filepath.ToSlash(target), true, nil
	}

	for _, entry := range entries {
		if !isSymlink(entry.mode) {
			continue
		}
		entryName := filepath.ToSlash(entry.relativePath)
		target := filepath.ToSlash(entry.linkTarget)
		if err := resolveSymlinkTarget(entryName, target, lookup); err != nil {
			return fmt.Errorf("archive symbolic link %q: %w", entryName, err)
		}
	}
	return nil
}

func resolveSymlinkTarget(entryName, target string, lookup symlinkTargetLookup) error {
	if err := validateSymlinkTargetSyntax(target); err != nil {
		return err
	}
	resolved := splitSymlinkPath(path.Dir(entryName))
	active := make(map[string]struct{})
	if err := resolveSymlinkComponents(&resolved, splitSymlinkPath(target), lookup, active); err != nil {
		if errors.Is(err, errSymlinkTargetEscapes) {
			return fmt.Errorf("target %q escapes the extraction root after symbolic-link resolution", target)
		}
		return err
	}
	return nil
}

var errSymlinkTargetEscapes = errors.New("symbolic link target escapes extraction root")

func resolveSymlinkComponents(
	resolved *[]string,
	components []string,
	lookup symlinkTargetLookup,
	active map[string]struct{},
) error {
	for _, component := range components {
		switch component {
		case "", ".":
			continue
		case "..":
			if len(*resolved) == 0 {
				return errSymlinkTargetEscapes
			}
			*resolved = (*resolved)[:len(*resolved)-1]
			continue
		}

		candidate := path.Join(append(*resolved, component)...)
		if lookup == nil {
			*resolved = append(*resolved, component)
			continue
		}
		target, isLink, err := lookup(candidate)
		if err != nil {
			return err
		}
		if !isLink {
			*resolved = append(*resolved, component)
			continue
		}
		if _, exists := active[candidate]; exists {
			return fmt.Errorf("symbolic link cycle through %q", candidate)
		}
		if len(active) >= maxSymlinkDepth {
			return fmt.Errorf("symbolic link resolution exceeds %d links", maxSymlinkDepth)
		}
		if err := validateSymlinkTargetSyntax(target); err != nil {
			return fmt.Errorf("symbolic link %q has invalid target: %w", candidate, err)
		}
		active[candidate] = struct{}{}
		err = resolveSymlinkComponents(resolved, splitSymlinkPath(target), lookup, active)
		delete(active, candidate)
		if err != nil {
			return err
		}
	}
	return nil
}

func validateSymlinkTargetSyntax(target string) error {
	if target == "" {
		return fmt.Errorf("target must not be empty")
	}
	if !utf8.ValidString(target) {
		return fmt.Errorf("target is not valid UTF-8")
	}
	if path.IsAbs(target) || strings.ContainsAny(target, "\\:\x00") {
		return fmt.Errorf("target %q is unsafe", target)
	}
	return nil
}

func splitSymlinkPath(value string) []string {
	if value == "." || value == "" {
		return nil
	}
	return strings.Split(value, "/")
}

func isSymlink(mode fs.FileMode) bool {
	return mode.Type() == fs.ModeSymlink
}

func preflightDestination(root *os.Root, entries []extractionEntry) error {
	for _, entry := range entries {
		if err := validateExistingParents(root, entry.relativePath); err != nil {
			return fmt.Errorf("archive entry %q: %w", entry.file.Name, err)
		}
		info, err := root.Lstat(entry.relativePath)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect archive destination %q: %w", entry.file.Name, err)
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			if !isSymlink(entry.mode) {
				return fmt.Errorf("archive destination %q is a symbolic link", entry.file.Name)
			}
			target, err := root.Readlink(entry.relativePath)
			if err != nil {
				return fmt.Errorf("read archive destination symbolic link %q: %w", entry.file.Name, err)
			}
			if target != entry.linkTarget {
				return fmt.Errorf(
					"archive symbolic link %q conflicts with existing target %q",
					entry.file.Name,
					target,
				)
			}
			continue
		}
		if entry.mode.IsDir() && !info.IsDir() {
			return fmt.Errorf("archive directory %q conflicts with an existing file", entry.file.Name)
		}
		if isSymlink(entry.mode) {
			return fmt.Errorf(
				"archive symbolic link %q conflicts with existing mode %s",
				entry.file.Name,
				info.Mode(),
			)
		}
		if !entry.mode.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("archive file %q conflicts with existing mode %s", entry.file.Name, info.Mode())
		}
	}
	return validateSymlinkTargets(root, entries)
}

func extractSymlink(root *os.Root, entry extractionEntry) error {
	info, err := root.Lstat(entry.relativePath)
	if errors.Is(err, fs.ErrNotExist) {
		if err := root.Symlink(entry.linkTarget, entry.relativePath); err != nil {
			return fmt.Errorf("create symbolic link: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect symbolic link destination: %w", err)
	}
	if info.Mode()&fs.ModeSymlink == 0 {
		return fmt.Errorf("destination has mode %s", info.Mode())
	}
	target, err := root.Readlink(entry.relativePath)
	if err != nil {
		return fmt.Errorf("read existing symbolic link: %w", err)
	}
	if target != entry.linkTarget {
		return fmt.Errorf("destination has target %q", target)
	}
	return nil
}

func validateExistingParents(root *os.Root, relativePath string) error {
	parent := filepath.Dir(relativePath)
	if parent == "." {
		return nil
	}
	current := ""
	for _, part := range strings.Split(parent, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := root.Lstat(current)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect parent directory %q: %w", current, err)
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("parent directory %q is a symbolic link", current)
		}
		if !info.IsDir() {
			return fmt.Errorf("parent path %q is not a directory", current)
		}
	}
	return nil
}

func extractRegularFile(ctx context.Context, root *os.Root, entry extractionEntry) error {
	parent := filepath.Dir(entry.relativePath)
	if parent != "." {
		if err := root.MkdirAll(parent, defaultDirectoryMode); err != nil {
			return fmt.Errorf("create parent directory: %w", err)
		}
	}

	source, err := entry.file.Open()
	if err != nil {
		return fmt.Errorf("open compressed contents: %w", err)
	}
	partial, partialPath, err := createPartialFile(root, entry.relativePath)
	if err != nil {
		_ = source.Close()
		return err
	}
	partialOpen := true
	defer func() {
		if partialOpen {
			_ = partial.Close()
		}
		_ = root.Remove(partialPath)
	}()

	written, copyErr := io.Copy(partial, contextReader{ctx: ctx, reader: source})
	sourceCloseErr := source.Close()
	if copyErr != nil {
		return fmt.Errorf("write decompressed contents: %w", copyErr)
	}
	if sourceCloseErr != nil {
		return fmt.Errorf("close compressed contents: %w", sourceCloseErr)
	}
	if uint64(written) != entry.file.UncompressedSize {
		return fmt.Errorf(
			"decompressed size is %d bytes, expected %d",
			written,
			entry.file.UncompressedSize,
		)
	}
	if err := partial.Chmod(archivePermissions(entry.mode)); err != nil {
		return fmt.Errorf("set decompressed file mode: %w", err)
	}
	if err := partial.Close(); err != nil {
		return fmt.Errorf("close decompressed file: %w", err)
	}
	partialOpen = false

	if err := publishRegularFile(root, partialPath, entry.relativePath); err != nil {
		return err
	}
	return nil
}

func createPartialFile(root *os.Root, target string) (*os.File, string, error) {
	var randomBytes [8]byte
	if _, err := rand.Read(randomBytes[:]); err != nil {
		return nil, "", fmt.Errorf("create partial file name: %w", err)
	}
	name := "." + filepath.Base(target) + "." + hex.EncodeToString(randomBytes[:]) + ".part"
	partialPath := filepath.Join(filepath.Dir(target), name)
	file, err := root.OpenFile(partialPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, "", fmt.Errorf("create partial file: %w", err)
	}
	return file, partialPath, nil
}

func publishRegularFile(root *os.Root, partialPath, target string) error {
	if err := root.Rename(partialPath, target); err == nil {
		return nil
	} else {
		info, inspectErr := root.Lstat(target)
		if inspectErr != nil {
			return fmt.Errorf("publish file: %w", err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("publish file: target has mode %s", info.Mode())
		}
		if removeErr := root.Remove(target); removeErr != nil {
			return fmt.Errorf("replace existing file: %w", removeErr)
		}
		if renameErr := root.Rename(partialPath, target); renameErr != nil {
			return fmt.Errorf("publish replacement file: %w", renameErr)
		}
		return nil
	}
}

func archivePermissions(mode fs.FileMode) fs.FileMode {
	permissions := mode.Perm() & safePermissionMask
	if permissions != 0 {
		return permissions
	}
	if mode.IsDir() {
		return defaultDirectoryMode
	}
	return defaultFileMode
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader contextReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(buffer)
}
