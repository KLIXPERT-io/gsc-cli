package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/KLIXPERT-io/gsc-cli/internal/config"
	"github.com/KLIXPERT-io/gsc-cli/internal/errs"
	"github.com/KLIXPERT-io/gsc-cli/internal/output"
	"github.com/KLIXPERT-io/gsc-cli/internal/update"
	"github.com/spf13/cobra"
)

func newUpdateCmd(version string) *cobra.Command {
	c := &cobra.Command{
		Use:   "update",
		Short: "Manage gsc self-update (status, check, apply)",
	}
	c.AddCommand(
		newUpdateStatusCmd(version),
		newUpdateCheckCmd(version),
		newUpdateApplyCmd(version),
	)
	return c
}

func resolveExecPath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		return r
	}
	return exe
}

func autoUpdateStatus(cfg *config.Config, execPath string) (enabled bool, reason string) {
	if v, ok := os.LookupEnv("GSC_NO_UPDATE"); ok && v != "" {
		switch v {
		case "0", "false":
		default:
			return false, "env:GSC_NO_UPDATE"
		}
	}
	if cfg != nil && !cfg.AutoUpdate {
		return false, "config:auto_update=false"
	}
	if execPath != "" && update.IsManagedInstall(execPath) {
		return false, "managed-install"
	}
	return true, ""
}

func newUpdateStatusCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current/latest version, last check, and auto-update state",
		RunE: func(cmd *cobra.Command, args []string) error {
			s := getState(cmd)
			ctx := cmd.Context()

			st, _ := update.LoadState()
			execPath := resolveExecPath()

			var cfg *config.Config
			if s != nil {
				cfg = s.Cfg
			}
			enabled, disabledReason := autoUpdateStatus(cfg, execPath)

			latest := ""
			latestErr := ""
			if tag, err := update.LatestRelease(ctx, version); err == nil {
				latest = tag
			} else {
				latest = "unknown"
				latestErr = err.Error()
			}

			data := map[string]any{
				"current_version":        version,
				"latest_version":         latest,
				"channel":                "stable",
				"last_check_at":          timeOrEmpty(st.LastCheckAt),
				"last_installed_version": st.LastInstalledVersion,
				"last_installed_at":      timeOrEmpty(st.LastInstalledAt),
				"auto_update_enabled":    enabled,
				"install_path":           execPath,
				"install_managed":        update.IsManagedInstall(execPath),
			}
			if !enabled {
				data["disabled_reason"] = disabledReason
			}
			if latestErr != "" && s != nil && s.Verbose {
				data["latest_error"] = latestErr
			}
			return emit(cmd, data, output.Meta{}, nil, nil)
		},
	}
}

func timeOrEmpty(t interface{ IsZero() bool }) any {
	if t.IsZero() {
		return ""
	}
	return t
}

func newUpdateCheckCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Force an update check now (bypasses the 24h throttle)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			res, err := update.CheckAndApply(ctx, version, true)
			if err != nil {
				fmt.Fprintf(os.Stderr, "gsc: update check failed: %v\n", err)
			}
			data := map[string]any{
				"updated": res.Updated,
				"from":    res.From,
				"to":      res.To,
				"reason":  res.Reason,
			}
			if err != nil {
				data["error"] = err.Error()
			}
			return emit(cmd, data, output.Meta{}, nil, nil)
		},
	}
}

func newUpdateApplyCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "apply",
		Short: "Force download + swap to the latest release",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			tag, err := update.LatestRelease(ctx, version)
			if err != nil {
				return errs.Newf(errs.CodeNetworkUnreachable, "resolve latest release: %v", err)
			}
			res, err := update.Apply(ctx, version, tag)
			if err != nil {
				return errs.Newf(errs.CodeGeneric, "apply update %s: %v", tag, err)
			}
			if res.Updated {
				fmt.Fprintf(os.Stdout, "gsc: updated to %s (was %s)\n", res.To, res.From)
			} else {
				fmt.Fprintf(os.Stdout, "gsc: no update applied (%s)\n", res.Reason)
			}
			return nil
		},
	}
}

