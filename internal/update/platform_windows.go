//go:build windows

package update

import (
	"os"
)

func atomicSwap(newPath, runningPath string) error {
	oldPath := runningPath + ".old"
	_ = os.Remove(oldPath)
	if err := os.Rename(runningPath, oldPath); err != nil {
		return err
	}
	if err := os.Rename(newPath, runningPath); err != nil {
		_ = os.Rename(oldPath, runningPath)
		return err
	}
	return nil
}

func isWritable(path string) bool {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

func isOwnedByCurrentUID(_ string) bool { return true }

func cleanupStaleSwap(runningPath string) {
	_ = os.Remove(runningPath + ".old")
}
