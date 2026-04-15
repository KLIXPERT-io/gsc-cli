//go:build windows

package update

import (
	"errors"
	"os"
	"time"
)

// On Windows we use a best-effort O_EXCL lockfile rather than LockFileEx;
// crash-leftover lockfiles are cleared on next run via a stale-age check.
type fileLock struct {
	path string
}

func acquireLock() (*fileLock, error) {
	d, err := stateDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(d, 0o755); err != nil {
		return nil, err
	}
	p, err := lockPath()
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			if info, statErr := os.Stat(p); statErr == nil {
				if time.Since(info.ModTime()) > 10*time.Minute {
					_ = os.Remove(p)
					return acquireLock()
				}
			}
			return nil, nil
		}
		return nil, err
	}
	_ = f.Close()
	return &fileLock{path: p}, nil
}

func (l *fileLock) Release() {
	if l == nil || l.path == "" {
		return
	}
	_ = os.Remove(l.path)
}
