// Package config loads ~/.config/gsc/config.toml and merges with flag/env/defaults per FR-10.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type Auth struct {
	CredentialsPath string `toml:"credentials_path"`
}

type Defaults struct {
	Property string `toml:"property"`
	Output   string `toml:"output"`
	Range    string `toml:"range"`
}

type Cache struct {
	Dir        string `toml:"dir"`
	DefaultTTL string `toml:"default_ttl"`
	TTLPSI     string `toml:"ttl_psi"`
	TTLCrUX    string `toml:"ttl_crux"`
}

type Logging struct {
	Verbose bool   `toml:"verbose"`
	Format  string `toml:"format"`
}

type Config struct {
	Auth       Auth     `toml:"auth"`
	Defaults   Defaults `toml:"defaults"`
	Cache      Cache    `toml:"cache"`
	Logging    Logging  `toml:"logging"`
	AutoUpdate bool     `toml:"auto_update"`
	// Path the config was loaded from (empty if defaults).
	path string `toml:"-"`
}

// Default returns built-in defaults.
func Default() *Config {
	return &Config{
		Defaults:   Defaults{Output: "json", Range: "last-28d"},
		Cache:      Cache{Dir: "./.gsc/cache", DefaultTTL: "15m", TTLPSI: "24h", TTLCrUX: "24h"},
		Logging:    Logging{Format: "text"},
		AutoUpdate: true,
	}
}

// AutoUpdateEnabled is the single source of truth for whether the background
// auto-updater (and `gsc update` apply paths) may run. It returns false when
// the env var GSC_NO_UPDATE is set to a non-empty value other than "0" or
// "false" (case-insensitive), or when c.AutoUpdate is false. Otherwise true.
// A nil *Config is treated as defaults (AutoUpdate=true).
func AutoUpdateEnabled(c *Config) bool {
	if v, ok := os.LookupEnv("GSC_NO_UPDATE"); ok && v != "" {
		switch strings.ToLower(v) {
		case "0", "false":
			// explicit off-of-off: treat as not set (do not disable)
		default:
			return false
		}
	}
	if c != nil && !c.AutoUpdate {
		return false
	}
	return true
}

func Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "gsc", "config.toml"), nil
}

// Load reads the config file; missing file = defaults.
func Load() (*Config, error) {
	c := Default()
	p, err := Path()
	if err != nil {
		return c, err
	}
	c.path = p
	b, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return c, nil
		}
		return c, err
	}
	if _, err := toml.Decode(string(b), c); err != nil {
		return c, fmt.Errorf("decode config: %w", err)
	}
	c.path = p
	return c, nil
}

// Save writes config back to disk (mkdir -p).
func (c *Config) Save() error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(c)
}

func (c *Config) LoadedPath() string { return c.path }

// TTL returns the cache default TTL parsed as duration (15m fallback).
func (c *Config) TTL() time.Duration {
	d, err := time.ParseDuration(strings.TrimSpace(c.Cache.DefaultTTL))
	if err != nil || d <= 0 {
		return 15 * time.Minute
	}
	return d
}

// PSITTL returns the PSI cache TTL (default 24h).
func (c *Config) PSITTL() time.Duration {
	d, err := time.ParseDuration(strings.TrimSpace(c.Cache.TTLPSI))
	if err != nil || d <= 0 {
		return 24 * time.Hour
	}
	return d
}

// CruxTTL returns the CrUX cache TTL (default 24h).
func (c *Config) CruxTTL() time.Duration {
	d, err := time.ParseDuration(strings.TrimSpace(c.Cache.TTLCrUX))
	if err != nil || d <= 0 {
		return 24 * time.Hour
	}
	return d
}

// Get returns the value at dotted key (e.g. "auth.credentials_path").
func (c *Config) Get(key string) (string, bool) {
	switch key {
	case "auth.credentials_path":
		return c.Auth.CredentialsPath, true
	case "defaults.property":
		return c.Defaults.Property, true
	case "defaults.output":
		return c.Defaults.Output, true
	case "defaults.range":
		return c.Defaults.Range, true
	case "cache.dir":
		return c.Cache.Dir, true
	case "cache.default_ttl":
		return c.Cache.DefaultTTL, true
	case "cache.ttl_psi":
		return c.Cache.TTLPSI, true
	case "cache.ttl_crux":
		return c.Cache.TTLCrUX, true
	case "logging.verbose":
		return fmt.Sprintf("%v", c.Logging.Verbose), true
	case "logging.format":
		return c.Logging.Format, true
	}
	return "", false
}

// Set updates a dotted key and saves.
func (c *Config) Set(key, value string) error {
	switch key {
	case "auth.credentials_path":
		c.Auth.CredentialsPath = value
	case "defaults.property":
		c.Defaults.Property = value
	case "defaults.output":
		c.Defaults.Output = value
	case "defaults.range":
		c.Defaults.Range = value
	case "cache.dir":
		c.Cache.Dir = value
	case "cache.default_ttl":
		if _, err := time.ParseDuration(value); err != nil {
			return fmt.Errorf("invalid duration: %q", value)
		}
		c.Cache.DefaultTTL = value
	case "cache.ttl_psi":
		if _, err := time.ParseDuration(value); err != nil {
			return fmt.Errorf("invalid duration: %q", value)
		}
		c.Cache.TTLPSI = value
	case "cache.ttl_crux":
		if _, err := time.ParseDuration(value); err != nil {
			return fmt.Errorf("invalid duration: %q", value)
		}
		c.Cache.TTLCrUX = value
	case "logging.verbose":
		c.Logging.Verbose = value == "true" || value == "1"
	case "logging.format":
		if value != "text" && value != "json" {
			return fmt.Errorf("logging.format must be text or json")
		}
		c.Logging.Format = value
	default:
		return fmt.Errorf("unknown key: %s", key)
	}
	return c.Save()
}

// Keys returns the list of known keys (stable order).
func Keys() []string {
	return []string{
		"auth.credentials_path",
		"defaults.property",
		"defaults.output",
		"defaults.range",
		"cache.dir",
		"cache.default_ttl",
		"cache.ttl_psi",
		"cache.ttl_crux",
		"logging.verbose",
		"logging.format",
	}
}

// ExpandHome expands a leading ~ in paths.
func ExpandHome(p string) string {
	if !strings.HasPrefix(p, "~") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, strings.TrimPrefix(p, "~"))
}
