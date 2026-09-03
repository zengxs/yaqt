package filelock

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestAcquireWaitsOnceAndHonorsContextCancellation(t *testing.T) {
	path := filepath.Join(newFileLockTestRoot(t), "entry.lock")
	firstFile, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open first lock file: %v", err)
	}
	first, err := Acquire(context.Background(), firstFile, nil)
	if err != nil {
		t.Fatalf("Acquire(first) error = %v", err)
	}
	defer func() {
		if err := first.Release(); err != nil {
			t.Errorf("release first lock: %v", err)
		}
	}()

	secondFile, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open second lock file: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	var waits atomic.Int32
	second, err := Acquire(ctx, secondFile, func() error {
		waits.Add(1)
		return nil
	})
	if second != nil {
		_ = second.Release()
		t.Fatal("Acquire(second) returned a lock while the first lock was held")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Acquire(second) error = %v, want context deadline", err)
	}
	if got, want := waits.Load(), int32(1); got != want {
		t.Errorf("onWait call count = %d, want %d", got, want)
	}
}

func TestReleaseAllowsAnotherAcquisition(t *testing.T) {
	path := filepath.Join(newFileLockTestRoot(t), "entry.lock")
	firstFile, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open first lock file: %v", err)
	}
	first, err := Acquire(context.Background(), firstFile, nil)
	if err != nil {
		t.Fatalf("Acquire(first) error = %v", err)
	}
	if err := first.Release(); err != nil {
		t.Fatalf("Release(first) error = %v", err)
	}

	secondFile, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open second lock file: %v", err)
	}
	second, err := Acquire(context.Background(), secondFile, nil)
	if err != nil {
		t.Fatalf("Acquire(second) error = %v", err)
	}
	if err := second.Release(); err != nil {
		t.Fatalf("Release(second) error = %v", err)
	}
}

func newFileLockTestRoot(t *testing.T) string {
	t.Helper()
	repositoryRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	parent := filepath.Join(repositoryRoot, ".tmp", "file-lock-tests")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatalf("create file lock test parent: %v", err)
	}
	directory, err := os.MkdirTemp(parent, "lock-*")
	if err != nil {
		t.Fatalf("create file lock test root: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(directory); err != nil {
			t.Errorf("remove file lock test root: %v", err)
		}
		_ = os.Remove(parent)
	})
	return directory
}
