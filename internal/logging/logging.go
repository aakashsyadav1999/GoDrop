// Package logging builds the application's slog.Logger from configuration.
package logging

import (
	"fmt"
	"io"
	"log/slog"
)

// New returns a logger that writes to w. level is debug, info, warn or error;
// format is "json" (one JSON object per line, for machines) or "text" (for people).
func New(w io.Writer, level, format string) (*slog.Logger, error) {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		return nil, fmt.Errorf("log level %q: must be debug, info, warn or error", level)
	}
	opts := &slog.HandlerOptions{Level: lvl}

	switch format {
	case "json":
		return slog.New(slog.NewJSONHandler(w, opts)), nil
	case "text":
		return slog.New(slog.NewTextHandler(w, opts)), nil
	default:
		return nil, fmt.Errorf("log format %q: must be json or text", format)
	}
}
