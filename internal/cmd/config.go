package cmd

import (
	"github.com/KLIXPERT-io/gsc-cli/internal/config"
	"github.com/KLIXPERT-io/gsc-cli/internal/errs"
	"github.com/KLIXPERT-io/gsc-cli/internal/output"
	"github.com/spf13/cobra"
)

func newConfigCmd() *cobra.Command {
	c := &cobra.Command{Use: "config", Short: "Read/write ~/.config/gsc/config.toml"}
	c.AddCommand(newConfigGetCmd(), newConfigSetCmd(), newConfigPathCmd(), newConfigListCmd())
	return c
}

func newConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Read a config value by dotted key (e.g. defaults.property)",
		Long: `Examples:
  gsc config get defaults.property
  gsc config get cache.dir`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			val, ok := cfg.Get(args[0])
			if !ok {
				return errs.New(errs.CodeInvalidArgs, "unknown key: "+args[0]).WithHint("Try `gsc config list`.")
			}
			return emit(cmd, map[string]any{"key": args[0], "value": val}, output.Meta{}, nil, nil)
		},
	}
}

func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Write a config value",
		Long: `Examples:
  gsc config set auth.credentials_path ~/secrets/gsc-client.json
  gsc config set defaults.property sc-domain:example.com
  gsc config set logging.format json`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if err := cfg.Set(args[0], args[1]); err != nil {
				return errs.New(errs.CodeInvalidArgs, err.Error())
			}
			return emit(cmd, map[string]any{"ok": true, "key": args[0], "value": args[1]}, output.Meta{}, nil, nil)
		},
	}
}

func newConfigPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the path of the config file",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := config.Path()
			if err != nil {
				return err
			}
			return emit(cmd, map[string]any{"path": p}, output.Meta{}, nil, nil)
		},
	}
}

func newConfigListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all known config keys and current values",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			out := map[string]any{}
			cols := []string{"key", "value"}
			rows := []output.Row{}
			for _, k := range config.Keys() {
				v, _ := cfg.Get(k)
				out[k] = v
				rows = append(rows, output.Row{"key": k, "value": v})
			}
			return emit(cmd, out, output.Meta{}, cols, rows)
		},
	}
}
