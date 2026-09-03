package cache

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveRootPrecedence(t *testing.T) {
	environmentRoot := filepath.Join(".tmp", "environment")
	t.Setenv(EnvironmentVariable, environmentRoot)

	explicitRoot := filepath.Join(".tmp", "explicit")
	got, err := ResolveRoot(explicitRoot)
	if err != nil {
		t.Fatalf("ResolveRoot(explicit) error = %v", err)
	}
	if got != filepath.Clean(explicitRoot) {
		t.Errorf("ResolveRoot(explicit) = %q, want %q", got, filepath.Clean(explicitRoot))
	}

	got, err = ResolveRoot("")
	if err != nil {
		t.Fatalf("ResolveRoot(environment) error = %v", err)
	}
	if got != filepath.Clean(environmentRoot) {
		t.Errorf("ResolveRoot(environment) = %q, want %q", got, filepath.Clean(environmentRoot))
	}
}

func TestResolveRootUsesOperatingSystemCache(t *testing.T) {
	t.Setenv(EnvironmentVariable, "")
	wantRoot, err := os.UserCacheDir()
	if err != nil {
		t.Fatalf("os.UserCacheDir() error = %v", err)
	}

	got, err := ResolveRoot("")
	if err != nil {
		t.Fatalf("ResolveRoot() error = %v", err)
	}
	if want := filepath.Join(wantRoot, "yaqt"); got != want {
		t.Errorf("ResolveRoot() = %q, want %q", got, want)
	}
}

func TestContextWithRoot(t *testing.T) {
	ctx := ContextWithRoot(context.Background(), filepath.Join("cache", "..", "cache"))
	got, ok := RootFromContext(ctx)
	if !ok {
		t.Fatal("RootFromContext() found no cache root")
	}
	if want := filepath.Clean("cache"); got != want {
		t.Errorf("RootFromContext() = %q, want %q", got, want)
	}
	if _, ok := RootFromContext(context.Background()); ok {
		t.Fatal("RootFromContext(background) found an unexpected cache root")
	}
	if _, ok := RootFromContext(ContextWithRoot(context.Background(), "")); ok {
		t.Fatal("RootFromContext(empty) found an unexpected cache root")
	}
}
