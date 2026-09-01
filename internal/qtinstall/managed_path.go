package qtinstall

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const managedTemporaryFileAttempts = 100

type managedPathRoot struct {
	path string
	root *os.Root
}

func openManagedPathRoot(path string, create bool) (*managedPathRoot, bool, error) {
	cleanPath, err := cleanManagedRootPath(path)
	if err != nil {
		return nil, false, err
	}
	if create {
		if err := os.MkdirAll(cleanPath, 0o755); err != nil {
			return nil, false, err
		}
	}
	root, err := os.OpenRoot(cleanPath)
	if errors.Is(err, os.ErrNotExist) && !create {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &managedPathRoot{path: cleanPath, root: root}, true, nil
}

func cleanManagedRootPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("managed path root must not be empty")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(absolute), nil
}

func relativeManagedPath(rootPath, path string) (string, error) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(rootPath, filepath.Clean(absolutePath))
	if err != nil {
		return "", err
	}
	if relative == ".." || filepath.IsAbs(relative) ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %s escapes managed root %s", path, rootPath)
	}
	return relative, nil
}

func (root *managedPathRoot) close() error {
	if root == nil || root.root == nil {
		return nil
	}
	err := root.root.Close()
	root.root = nil
	return err
}

func (root *managedPathRoot) relativePath(path string) (string, error) {
	if root == nil || root.root == nil {
		return "", fmt.Errorf("managed path root is not open")
	}
	return relativeManagedPath(root.path, path)
}

func (root *managedPathRoot) ensureDirectory(path, description string) error {
	relative, err := root.relativePath(path)
	if err != nil {
		return err
	}
	if err := root.root.MkdirAll(relative, 0o755); err != nil {
		return fmt.Errorf("create %s %s: %w", description, path, err)
	}
	_, err = root.inspectDirectory(path, description)
	return err
}

func (root *managedPathRoot) inspectDirectory(path, description string) (bool, error) {
	relative, err := root.relativePath(path)
	if err != nil {
		return false, err
	}
	info, err := root.root.Stat(relative)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect %s %s: %w", description, path, err)
	}
	if !info.IsDir() {
		return false, fmt.Errorf("%s %s is not a directory", description, path)
	}
	return true, nil
}

func (root *managedPathRoot) inspectResolvedDirectory(path, description string) (bool, error) {
	relative, err := root.relativePath(path)
	if err != nil {
		return false, err
	}
	info, err := root.root.Stat(relative)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect %s %s: %w", description, path, err)
	}
	if !info.IsDir() {
		return false, fmt.Errorf("%s %s is not a directory", description, path)
	}
	return true, nil
}

func (root *managedPathRoot) inspectFile(path, description string) (bool, error) {
	relative, err := root.relativePath(path)
	if err != nil {
		return false, err
	}
	info, err := root.root.Stat(relative)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect %s %s: %w", description, path, err)
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("%s %s is not a regular file", description, path)
	}
	return true, nil
}

func (root *managedPathRoot) openFile(path string, flag int, permission fs.FileMode) (*os.File, error) {
	relative, err := root.relativePath(path)
	if err != nil {
		return nil, err
	}
	return root.root.OpenFile(relative, flag, permission)
}

func (root *managedPathRoot) createTemporaryFile(
	directory string,
	prefix string,
) (*os.File, string, error) {
	relativeDirectory, err := root.relativePath(directory)
	if err != nil {
		return nil, "", err
	}
	for range managedTemporaryFileAttempts {
		var randomBytes [8]byte
		if _, err := rand.Read(randomBytes[:]); err != nil {
			return nil, "", err
		}
		name := prefix + hex.EncodeToString(randomBytes[:]) + ".tmp"
		relativePath := filepath.Join(relativeDirectory, name)
		file, err := root.root.OpenFile(relativePath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if err == nil {
			return file, relativePath, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, "", err
		}
	}
	return nil, "", fmt.Errorf("could not create a unique temporary file in %s", directory)
}

func (root *managedPathRoot) remove(relativePath string) error {
	return root.root.Remove(relativePath)
}

func (root *managedPathRoot) rename(oldRelativePath, newPath string) error {
	newRelativePath, err := root.relativePath(newPath)
	if err != nil {
		return err
	}
	return root.root.Rename(oldRelativePath, newRelativePath)
}
