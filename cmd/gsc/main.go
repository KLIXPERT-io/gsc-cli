package main

import (
	"os"

	"github.com/KLIXPERT-io/gsc-cli/internal/cmd"
)

// Version is injected at build time via -ldflags.
var version = "dev"

func main() {
	os.Exit(cmd.Execute(version))
}
