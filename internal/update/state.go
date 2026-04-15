// Package update implements the background self-update flow for the gsc CLI.
//
// See docs/feature/auto-update/feature.md for the full spec. Public entry
// points are Background, CheckAndApply, Apply, and LatestRelease.
package update

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// State mirrors the on-disk JSON shape from spec §6.
type State struct {
	LastCheckAt          time.Time `json:"last_check_at,omitempty"`
	LastInstalledVersion string    `json:"last_installed_version,omitempty"`
	LastInstalledAt      time.Time `json:"last_installed_at,omitempty"`
	InstallManaged       bool      `json:"install_managed,omitempty"`
	InstallPath          string    `json:"install_path,omitempty"`
}

// stateDir returns the directory that holds update-state.json.
func stateDir() (string, error) {
	cfg, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cfg, "gsc"), nil
}

func statePath() (string, error) {
	d, err := stateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "update-state.json"), nil
}

func lockPath() (string, error) {
	d, err := stateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "update-state.lock"), nil
}

// LoadState returns the persisted state, or a zero-value State if missing.
func LoadState() (State, error) {
	var s State
	p, err := statePath()
	if err != nil {
		return s, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		return s, err
	}
	if len(b) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return State{}, err
	}
	return s, nil
}

// SaveState writes state atomically (write to temp + rename in same dir).
func SaveState(s State) error {
	d, err := stateDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(d, 0o755); err != nil {
		return err
	}
	p := filepath.Join(d, "update-state.json")
	tmp, err := os.CreateTemp(d, "update-state-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(&s); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, p)
}
