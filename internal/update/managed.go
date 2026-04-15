package update

import (
	"runtime"
	"strings"
)

var unixManagedPrefixes = []string{
	"/opt/homebrew",
	"/usr/local/Cellar",
	"/home/linuxbrew",
	"/snap",
	"/var/lib/flatpak",
}

var windowsManagedPrefixes = []string{
	`C:\Program Files`,
	`C:\ProgramData\chocolatey`,
}

// IsManagedInstall reports whether the binary at path looks installed by a
// system package manager and should be left alone (FR-004).
func IsManagedInstall(path string) bool {
	if path == "" {
		return false
	}
	if runtime.GOOS == "windows" {
		lower := strings.ToLower(path)
		for _, p := range windowsManagedPrefixes {
			if strings.HasPrefix(strings.ToLower(path), strings.ToLower(p)) {
				return true
			}
		}
		// Per-user Scoop install lives under %USERPROFILE%\scoop\.
		if strings.Contains(lower, `\scoop\`) {
			return true
		}
		return false
	}
	for _, p := range unixManagedPrefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// installLooksUpdatable bundles managed-prefix, writability, and ownership checks.
func installLooksUpdatable(path string) (ok bool, reason string) {
	if IsManagedInstall(path) {
		return false, "managed-install"
	}
	if !isWritable(path) {
		return false, "non-writable"
	}
	if !isOwnedByCurrentUID(path) {
		return false, "non-writable"
	}
	return true, ""
}
