package qtinstall

import (
	"fmt"
	"os"
	"path/filepath"
)

const cacheDirEnvironmentVariable = "YAQT_CACHE_DIR"

// ResolveCacheDir returns the cache root selected by an explicit override, the
// YAQT_CACHE_DIR environment variable, or the operating system cache directory.
func ResolveCacheDir(override string) (string, error) {
	if override != "" {
		return filepath.Clean(override), nil
	}
	if environment := os.Getenv(cacheDirEnvironmentVariable); environment != "" {
		return filepath.Clean(environment), nil
	}

	root, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache directory: %w", err)
	}
	return filepath.Join(root, "yaqt"), nil
}
