//go:build windows

package quota

import (
	"os"

	"golang.org/x/sys/windows"
)

// LockFileEx with LOCKFILE_EXCLUSIVE_LOCK gives us exclusive cross-process locking.
func lockFile(f *os.File) error {
	ol := new(windows.Overlapped)
	return windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK,
		0,
		^uint32(0), ^uint32(0),
		ol,
	)
}

func unlockFile(f *os.File) error {
	ol := new(windows.Overlapped)
	return windows.UnlockFileEx(
		windows.Handle(f.Fd()),
		0,
		^uint32(0), ^uint32(0),
		ol,
	)
}
