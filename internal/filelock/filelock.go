// Package filelock provides context-aware advisory file locks.
package filelock

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"
)

const retryInterval = 100 * time.Millisecond

// Lock holds an advisory operating system lock and owns its file handle.
type Lock struct {
	file   *os.File
	unlock func() error
}

// OpenFile creates or opens a regular lock file beneath root without following
// a symbolic link outside root.
func OpenFile(root *os.Root, name string, permission fs.FileMode) (*os.File, error) {
	if root == nil {
		return nil, fmt.Errorf("lock file root is not open")
	}
	file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR, permission)
	if err == nil {
		return validateFile(file, name)
	}
	if !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	file, err = root.OpenFile(name, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	return validateFile(file, name)
}

func validateFile(file *os.File, name string) (*os.File, error) {
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, fmt.Errorf("lock file %s is not a regular file", name)
	}
	return file, nil
}

// Acquire waits until file can be locked or ctx is canceled. onWait is called
// at most once, after the first unsuccessful acquisition attempt.
func Acquire(ctx context.Context, file *os.File, onWait func() error) (*Lock, error) {
	if file == nil {
		return nil, fmt.Errorf("advisory lock file is not open")
	}
	closeWithError := func(cause error) (*Lock, error) {
		if closeErr := file.Close(); closeErr != nil {
			return nil, errors.Join(cause, closeErr)
		}
		return nil, cause
	}
	if err := ctx.Err(); err != nil {
		return closeWithError(err)
	}

	waitReported := false
	for {
		unlock, acquired, err := tryLock(file)
		if err != nil {
			return closeWithError(err)
		}
		if acquired {
			return &Lock{file: file, unlock: unlock}, nil
		}
		if !waitReported && onWait != nil {
			if err := onWait(); err != nil {
				return closeWithError(err)
			}
			waitReported = true
		}

		timer := time.NewTimer(retryInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return closeWithError(ctx.Err())
		case <-timer.C:
		}
	}
}

// Release unlocks the file and closes its handle.
func (lock *Lock) Release() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	var result error
	if lock.unlock != nil {
		result = lock.unlock()
	}
	if err := lock.file.Close(); err != nil {
		result = errors.Join(result, err)
	}
	lock.file = nil
	lock.unlock = nil
	return result
}
