package main

import (
	"context"
	"os"

	"github.com/KLIXPERT-io/gsc-cli/internal/cmd"
	"github.com/KLIXPERT-io/gsc-cli/internal/config"
	"github.com/KLIXPERT-io/gsc-cli/internal/update"
)

// Version is injected at build time via -ldflags.
var version = "dev"

func main() {
	// Best-effort background auto-update launch (FR-001). Errors loading
	// config default to opted-out so main never crashes here.
	optedOut := true
	if cfg, err := config.Load(); err == nil {
		optedOut = !config.AutoUpdateEnabled(cfg)
	}
	update.Background(context.Background(), version, optedOut)

	os.Exit(cmd.Execute(version))
}
