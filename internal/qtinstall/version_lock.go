package qtinstall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

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
		if err := lock.release(); err != nil {
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
) (*advisoryFileLock, error) {
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
	if _, err := managedRoot.inspectFile(lockPath, "Qt version lock"); err != nil {
		return nil, fmt.Errorf("inspect Qt %s installation lock: %w", version, err)
	}
	file, err := managedRoot.openFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open Qt %s installation lock %s: %w", version, lockPath, err)
	}
	lock, err := acquireAdvisoryFileLock(ctx, file, onWait)
	if err != nil {
		return nil, fmt.Errorf("lock Qt %s installation: %w", version, err)
	}
	if err := writeVersionLockMetadata(file); err != nil {
		if releaseErr := lock.release(); releaseErr != nil {
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
