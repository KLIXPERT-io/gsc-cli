// Package logging configures slog to stderr per FR-12.
package logging

import (
	"io"
	"log/slog"
	"os"
)

type Options struct {
	Verbose bool
	Quiet   bool
	Format  string // "text" or "json"
}

// Setup returns a configured slog.Logger writing to stderr.
func Setup(opts Options) *slog.Logger {
	var w io.Writer = os.Stderr
	level := slog.LevelInfo
	switch {
	case opts.Verbose:
		level = slog.LevelDebug
	case opts.Quiet:
		level = slog.LevelError
	}
	handlerOpts := &slog.HandlerOptions{Level: level}
	var h slog.Handler
	if opts.Format == "json" {
		h = slog.NewJSONHandler(w, handlerOpts)
	} else {
		h = slog.NewTextHandler(w, handlerOpts)
	}
	l := slog.New(h)
	slog.SetDefault(l)
	return l
}
