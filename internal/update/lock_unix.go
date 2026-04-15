//go:build !windows

package update

import (
	"os"

	"golang.org/x/sys/unix"
)

type fileLock struct {
	f *os.File
}

// acquireLock takes an exclusive non-blocking advisory lock on update-state.lock.
// Returns (nil, nil) if another process already holds it (caller should bail out).
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
	f, err := os.OpenFile(p, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		if err == unix.EWOULDBLOCK {
			return nil, nil
		}
		return nil, err
	}
	return &fileLock{f: f}, nil
}

func (l *fileLock) Release() {
	if l == nil || l.f == nil {
		return
	}
	_ = unix.Flock(int(l.f.Fd()), unix.LOCK_UN)
	_ = l.f.Close()
}
