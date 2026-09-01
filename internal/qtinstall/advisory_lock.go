package qtinstall

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
)

const advisoryFileLockRetryInterval = 100 * time.Millisecond

type advisoryFileLock struct {
	file   *os.File
	unlock func() error
}

func acquireAdvisoryFileLock(
	ctx context.Context,
	file *os.File,
	onWait func() error,
) (*advisoryFileLock, error) {
	if file == nil {
		return nil, fmt.Errorf("advisory lock file is not open")
	}
	closeWithError := func(cause error) (*advisoryFileLock, error) {
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
		unlock, acquired, err := tryFileLock(file)
		if err != nil {
			return closeWithError(err)
		}
		if acquired {
			return &advisoryFileLock{file: file, unlock: unlock}, nil
		}
		if !waitReported && onWait != nil {
			if err := onWait(); err != nil {
				return closeWithError(err)
			}
			waitReported = true
		}

		timer := time.NewTimer(advisoryFileLockRetryInterval)
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

func (lock *advisoryFileLock) release() error {
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
