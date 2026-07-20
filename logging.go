package main

import (
	"io"
	"log/slog"
	"os"
)

// logLevel is the runtime-adjustable log level: the Settings debug toggle
// flips it between Info and Debug live, so there is no startup-only flag
// to forget (loggers pick the change up immediately via the LevelVar).
var logLevel = new(slog.LevelVar)

func init() {
	slog.SetDefault(newLogger(os.Stderr))
}

// newLogger builds the app's text logger: microsecond timestamps, level
// gated by logLevel.
func newLogger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: logLevel,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				a.Value = slog.StringValue(a.Value.Time().Format("2006/01/02 15:04:05.000000"))
			}
			return a
		},
	}))
}
