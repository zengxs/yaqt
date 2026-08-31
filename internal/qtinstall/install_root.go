package qtinstall

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zengxs/yaqt/internal/qtrepo"
)

// ResolveInstallRoot normalizes the directory that contains versioned Qt
// installations. The root must not include the requested version directory.
func ResolveInstallRoot(value string, version qtrepo.Version) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("Qt installation root must not be empty")
	}
	if strings.ContainsAny(value, "\r\n\x00") {
		return "", fmt.Errorf("Qt installation root contains unsupported characters")
	}

	root, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolve Qt installation root %s: %w", value, err)
	}
	if filepath.Base(root) == version.String() {
		return "", fmt.Errorf(
			"Qt installation root %s includes version %s; use %s instead",
			root,
			version,
			filepath.Dir(root),
		)
	}
	info, err := os.Stat(root)
	if err == nil {
		if !info.IsDir() {
			return "", fmt.Errorf("Qt installation root %s is not a directory", root)
		}
		return root, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect Qt installation root %s: %w", root, err)
	}
	return root, nil
}
