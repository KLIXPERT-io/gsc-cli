package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

const (
	releaseAPI       = "https://api.github.com/repos/KLIXPERT-io/gsc-cli/releases/latest"
	assetURLTemplate = "https://github.com/KLIXPERT-io/gsc-cli/releases/download/%s/%s"
	checkInterval    = 24 * time.Hour
	apiTimeout       = 5 * time.Second
	downloadTimeout  = 60 * time.Second
)

// Result describes the outcome of an update attempt.
type Result struct {
	Updated bool
	From    string
	To      string
	Reason  string
}

type ghRelease struct {
	TagName    string `json:"tag_name"`
	Prerelease bool   `json:"prerelease"`
}

// Background launches a detached goroutine that runs CheckAndApply, honoring
// the opt-out flag and skipping dev builds. It never blocks the caller and
// never panics out.
func Background(ctx context.Context, currentVersion string, optedOut bool) {
	if optedOut || currentVersion == "" || currentVersion == "dev" {
		return
	}
	go func() {
		defer func() { _ = recover() }()
		_, err := CheckAndApply(ctx, currentVersion, false)
		if err != nil {
			debugf("update: %v", err)
		}
	}()
}

// CheckAndApply consults the GitHub releases/latest endpoint, applies the
// 24h throttle (unless force), and runs the download+verify+swap pipeline
// when a newer stable tag is available.
func CheckAndApply(ctx context.Context, currentVersion string, force bool) (Result, error) {
	res := Result{From: currentVersion}

	exe, err := resolveBinary()
	if err != nil {
		return res, err
	}
	cleanupStaleSwap(exe)

	st, _ := LoadState()
	st.InstallPath = exe

	if !force && !st.LastCheckAt.IsZero() && time.Since(st.LastCheckAt) < checkInterval {
		res.Reason = "throttled"
		return res, nil
	}

	if ok, why := installLooksUpdatable(exe); !ok {
		st.InstallManaged = (why == "managed-install")
		st.LastCheckAt = time.Now().UTC()
		_ = SaveState(st)
		res.Reason = why
		return res, nil
	}

	lock, lockErr := acquireLock()
	if lockErr != nil {
		return res, lockErr
	}
	if lock == nil {
		res.Reason = "locked"
		return res, nil
	}
	defer lock.Release()

	tag, err := LatestRelease(ctx, currentVersion)
	if err != nil {
		st.LastCheckAt = time.Now().UTC()
		_ = SaveState(st)
		return res, err
	}
	res.To = tag

	if !isNewer(tag, currentVersion) {
		st.LastCheckAt = time.Now().UTC()
		_ = SaveState(st)
		res.Reason = "up-to-date"
		return res, nil
	}

	if err := downloadAndSwap(ctx, tag, exe); err != nil {
		st.LastCheckAt = time.Now().UTC()
		_ = SaveState(st)
		return res, err
	}

	now := time.Now().UTC()
	st.LastCheckAt = now
	st.LastInstalledAt = now
	st.LastInstalledVersion = tag
	_ = SaveState(st)

	res.Updated = true
	res.Reason = "applied"
	return res, nil
}

// Apply forces a download+verify+swap of a specific tag, regardless of the
// 24h throttle. Managed-install and writability guards still apply.
func Apply(ctx context.Context, currentVersion, targetTag string) (Result, error) {
	res := Result{From: currentVersion, To: targetTag}

	exe, err := resolveBinary()
	if err != nil {
		return res, err
	}
	cleanupStaleSwap(exe)

	if ok, why := installLooksUpdatable(exe); !ok {
		res.Reason = why
		return res, fmt.Errorf("install not updatable: %s", why)
	}

	lock, lockErr := acquireLock()
	if lockErr != nil {
		return res, lockErr
	}
	if lock == nil {
		res.Reason = "locked"
		return res, errors.New("another update is already in progress")
	}
	defer lock.Release()

	if err := downloadAndSwap(ctx, targetTag, exe); err != nil {
		return res, err
	}

	now := time.Now().UTC()
	st, _ := LoadState()
	st.InstallPath = exe
	st.LastCheckAt = now
	st.LastInstalledAt = now
	st.LastInstalledVersion = targetTag
	_ = SaveState(st)

	res.Updated = true
	res.Reason = "applied"
	return res, nil
}

// LatestRelease returns the latest stable tag (e.g. "v1.2.3") from GitHub.
func LatestRelease(ctx context.Context, currentVersion string) (string, error) {
	rctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(rctx, http.MethodGet, releaseAPI, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "gsc-cli/"+currentVersion)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github releases: status %d", resp.StatusCode)
	}
	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", err
	}
	if rel.Prerelease {
		return "", errors.New("github returned prerelease for /releases/latest")
	}
	if rel.TagName == "" {
		return "", errors.New("github release missing tag_name")
	}
	return rel.TagName, nil
}

func resolveBinary() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}

func isNewer(latestTag, currentVersion string) bool {
	cur := normalizeSemver(currentVersion)
	lat := normalizeSemver(latestTag)
	if !semver.IsValid(cur) || !semver.IsValid(lat) {
		return false
	}
	return semver.Compare(lat, cur) > 0
}

func normalizeSemver(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return v
}

func archiveAssetName(tag string) string {
	v := strings.TrimPrefix(tag, "v")
	ext := "tar.gz"
	if runtime.GOOS == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("gsc_%s_%s_%s.%s", v, runtime.GOOS, runtime.GOARCH, ext)
}

func downloadAndSwap(ctx context.Context, tag, runningPath string) error {
	tmp, err := os.MkdirTemp(os.TempDir(), "gsc-update-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	assetName := archiveAssetName(tag)
	assetURL := fmt.Sprintf(assetURLTemplate, tag, assetName)
	sumsURL := fmt.Sprintf(assetURLTemplate, tag, "checksums.txt")

	archivePath := filepath.Join(tmp, assetName)
	if err := downloadFile(ctx, assetURL, archivePath); err != nil {
		return err
	}
	sumsPath := filepath.Join(tmp, "checksums.txt")
	if err := downloadFile(ctx, sumsURL, sumsPath); err != nil {
		return err
	}

	want, err := lookupChecksum(sumsPath, assetName)
	if err != nil {
		return err
	}
	got, err := sha256File(archivePath)
	if err != nil {
		return err
	}
	if !strings.EqualFold(want, got) {
		return fmt.Errorf("checksum mismatch for %s", assetName)
	}

	binName := "gsc"
	if runtime.GOOS == "windows" {
		binName = "gsc.exe"
	}
	stagingDir := filepath.Dir(runningPath)
	stagedBin, err := os.CreateTemp(stagingDir, ".gsc-new-*")
	if err != nil {
		// Fall back to temp dir if target dir is not writable for temp creation.
		stagedBin, err = os.CreateTemp(tmp, ".gsc-new-*")
		if err != nil {
			return err
		}
	}
	stagedPath := stagedBin.Name()
	stagedBin.Close()
	os.Remove(stagedPath)

	if runtime.GOOS == "windows" {
		if err := extractZip(archivePath, binName, stagedPath); err != nil {
			return err
		}
	} else {
		if err := extractTarGz(archivePath, binName, stagedPath); err != nil {
			return err
		}
	}

	if err := atomicSwap(stagedPath, runningPath); err != nil {
		os.Remove(stagedPath)
		return err
	}
	return nil
}

func downloadFile(ctx context.Context, url, dst string) error {
	dctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(dctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: status %d", url, resp.StatusCode)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return err
	}
	return nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// lookupChecksum parses a `<sha256>  <filename>` line set and returns the
// hex digest matching the asset name.
func lookupChecksum(sumsPath, assetName string) (string, error) {
	b, err := os.ReadFile(sumsPath)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := fields[len(fields)-1]
		name = strings.TrimPrefix(name, "*")
		if filepath.Base(name) == assetName {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("checksum not found for %s", assetName)
}

func extractTarGz(archivePath, wantName, dst string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("binary %s not found in archive", wantName)
		}
		if err != nil {
			return err
		}
		if filepath.Base(hdr.Name) != wantName {
			continue
		}
		return writeFromReader(tr, dst, 0o755)
	}
}

func extractZip(archivePath, wantName, dst string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, zf := range zr.File {
		if filepath.Base(zf.Name) != wantName {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		err = writeFromReader(rc, dst, 0o755)
		rc.Close()
		return err
	}
	return fmt.Errorf("binary %s not found in archive", wantName)
}

func writeFromReader(r io.Reader, dst string, mode os.FileMode) error {
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func debugf(format string, args ...any) {
	if os.Getenv("GSC_UPDATE_DEBUG") == "" {
		return
	}
	fmt.Fprintf(os.Stderr, "[gsc-update] "+format+"\n", args...)
}
