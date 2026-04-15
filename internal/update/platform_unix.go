//go:build !windows

package update

import (
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func atomicSwap(newPath, runningPath string) error {
	if err := os.Chmod(newPath, 0o755); err != nil {
		return err
	}
	return os.Rename(newPath, runningPath)
}

func isWritable(path string) bool {
	return unix.Access(path, unix.W_OK) == nil
}

func isOwnedByCurrentUID(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return true
	}
	return int(st.Uid) == os.Getuid()
}

func cleanupStaleSwap(_ string) {}
