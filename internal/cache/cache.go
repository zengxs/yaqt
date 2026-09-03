// Package cache resolves and carries the shared yaqt cache root.
package cache

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// EnvironmentVariable names the environment variable that overrides the yaqt
// cache root.
const EnvironmentVariable = "YAQT_CACHE_DIR"

type rootContextKey struct{}

// ResolveRoot returns the cache root selected by an explicit override, the
// YAQT_CACHE_DIR environment variable, or the operating system cache directory.
func ResolveRoot(override string) (string, error) {
	if override != "" {
		return filepath.Clean(override), nil
	}
	if environment := os.Getenv(EnvironmentVariable); environment != "" {
		return filepath.Clean(environment), nil
	}

	root, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache directory: %w", err)
	}
	return filepath.Join(root, "yaqt"), nil
}

// ContextWithRoot returns a child context that selects root for cache-backed
// operations performed as part of the request.
func ContextWithRoot(ctx context.Context, root string) context.Context {
	if root == "" {
		return ctx
	}
	return context.WithValue(ctx, rootContextKey{}, filepath.Clean(root))
}

// RootFromContext returns the cache root attached to ctx, if any.
func RootFromContext(ctx context.Context) (string, bool) {
	root, ok := ctx.Value(rootContextKey{}).(string)
	return root, ok && root != ""
}
