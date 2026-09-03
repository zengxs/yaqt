package qtinstall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/zengxs/yaqt/internal/filelock"
	"github.com/zengxs/yaqt/internal/qtrepo"
)

type versionLockMetadata struct {
	ProcessID int       `json:"process_id"`
	StartedAt time.Time `json:"started_at"`
	Command   []string  `json:"command"`
}

func withVersionLock(
	ctx context.Context,
	root string,
	version qtrepo.Version,
	onWait func() error,
	operation func() error,
) (resultErr error) {
	if operation == nil {
		return fmt.Errorf("version-locked installation operation is not configured")
	}
	lock, err := acquireVersionLock(ctx, root, version, onWait)
	if err != nil {
		return err
	}
	defer func() {
		if err := lock.Release(); err != nil {
			resultErr = errors.Join(
				resultErr,
				fmt.Errorf("release Qt %s installation lock: %w", version, err),
			)
		}
	}()
	return operation()
}

func acquireVersionLock(
	ctx context.Context,
	root string,
	version qtrepo.Version,
	onWait func() error,
) (*filelock.Lock, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("acquire Qt %s installation lock: %w", version, err)
	}
	managedRoot, _, err := openManagedPathRoot(root, true)
	if err != nil {
		return nil, fmt.Errorf("open Qt installation root %s: %w", root, err)
	}
	defer func() { _ = managedRoot.close() }()
	stateDirectory := filepath.Join(root, version.String(), ".yaqt")
	if err := managedRoot.ensureDirectory(stateDirectory, "Qt version state directory"); err != nil {
		return nil, fmt.Errorf("create Qt %s installation state directory: %w", version, err)
	}
	lockPath := filepath.Join(stateDirectory, "install.lock")
	relativeLockPath, err := managedRoot.relativePath(lockPath)
	if err != nil {
		return nil, fmt.Errorf("resolve Qt %s installation lock %s: %w", version, lockPath, err)
	}
	file, err := filelock.OpenFile(managedRoot.root, relativeLockPath, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open Qt %s installation lock %s: %w", version, lockPath, err)
	}
	lock, err := filelock.Acquire(ctx, file, onWait)
	if err != nil {
		return nil, fmt.Errorf("lock Qt %s installation: %w", version, err)
	}
	if err := writeVersionLockMetadata(file); err != nil {
		if releaseErr := lock.Release(); releaseErr != nil {
			err = errors.Join(err, releaseErr)
		}
		return nil, fmt.Errorf("write Qt %s installation lock metadata: %w", version, err)
	}
	return lock, nil
}

func writeVersionLockMetadata(file *os.File) error {
	if err := file.Truncate(0); err != nil {
		return err
	}
	if _, err := file.Seek(0, 0); err != nil {
		return err
	}
	metadata := versionLockMetadata{
		ProcessID: os.Getpid(),
		StartedAt: time.Now().UTC(),
		Command:   append([]string(nil), os.Args...),
	}
	if err := json.NewEncoder(file).Encode(metadata); err != nil {
		return err
	}
	return file.Sync()
}
