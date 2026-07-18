// Package logging provides a small colorized console logger with a couple
// of layout helpers (Section/Subsection) for readable CLI output - the Go
// equivalent of the original tool's Utils::ColorizedLogger, built on the
// standard library's log/slog rather than a bespoke logger.
//
// This logger is for use OUTSIDE Temporal workflow code (the CLI itself,
// and inside activities via a thin adapter). Workflow functions must use
// workflow.GetLogger(ctx) instead - see internal/workflows - because
// Temporal suppresses duplicate logging on workflow replay only when you
// go through its own logger.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/mattn/go-isatty"
)

// levelFatal sits above slog.LevelError - logging at this level also exits
// the process, matching the original logger's fatal().
const levelFatal = slog.Level(12)

// Logger embeds *slog.Logger so Debug/Info/Warn/Error are promoted for
// free, with the exact "(msg string, keyvals ...any)" signatures that
// satisfy both this codebase's small logging interfaces and
// go.temporal.io/sdk/log.Logger.
type Logger struct {
	*slog.Logger
}

// New builds a Logger writing to out, filtering below level.
func New(out io.Writer, level slog.Level) *Logger {
	handler := &colorHandler{out: out, level: level, color: shouldColor(out)}
	return &Logger{Logger: slog.New(handler)}
}

// Fatal logs message (plus err's text, if given) at the highest severity and
// exits the process with status 1.
func (l *Logger) Fatal(message string, err error) {
	if err != nil {
		message = fmt.Sprintf("%s (%v)", message, err)
	}
	l.Logger.Log(context.Background(), levelFatal, message)
	os.Exit(1)
}

// Section prints a heavy banner around message - for top-level phase
// announcements like "Starting Workflow".
func (l *Logger) Section(message string) {
	bar := strings.Repeat("=", max(80, len(message)))
	l.Info("")
	l.Info(bar)
	l.Info(message)
	l.Info(bar)
	l.Info("")
}

// Subsection prints a lighter banner - for per-orchestrator/per-phase headers.
func (l *Logger) Subsection(heading, subheading string) {
	width := max(40, len(heading), len(subheading))
	l.Info("")
	l.Info(heading)
	l.Info(strings.Repeat("-", width))
	if subheading != "" {
		l.Info(subheading)
		l.Info("")
	}
}

// colorHandler is a minimal slog.Handler rendering one colorized line per
// record: "<timestamp> [LEVEL] message key=value ...". It doesn't
// accumulate attrs from WithAttrs/WithGroup - this tool only ever logs
// plain messages, so that's not worth the extra bookkeeping.
type colorHandler struct {
	out   io.Writer
	level slog.Level
	color bool
}

func (h *colorHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *colorHandler) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	b.WriteString(r.Time.Format("2006-01-02 15:04:05"))
	b.WriteByte(' ')
	b.WriteString(h.severityTag(r.Level))
	b.WriteByte(' ')
	b.WriteString(r.Message)

	r.Attrs(func(a slog.Attr) bool {
		fmt.Fprintf(&b, " %s=%v", a.Key, a.Value.Any())
		return true
	})
	b.WriteByte('\n')

	_, err := io.WriteString(h.out, b.String())
	return err
}

func (h *colorHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *colorHandler) WithGroup(string) slog.Handler      { return h }

func (h *colorHandler) severityTag(level slog.Level) string {
	var c *color.Color
	var tag string

	switch {
	case level < slog.LevelInfo:
		c, tag = color.New(color.FgHiBlack), "[DEBUG]"
	case level < slog.LevelWarn:
		c, tag = color.New(color.FgCyan), "[INFO] "
	case level < slog.LevelError:
		c, tag = color.New(color.FgYellow), "[WARN] "
	case level < levelFatal:
		c, tag = color.New(color.FgRed), "[ERROR]"
	default:
		c, tag = color.New(color.FgHiRed), "[FATAL]"
	}

	if h.color {
		c.EnableColor()
	} else {
		c.DisableColor()
	}
	return c.Sprint(tag)
}

// shouldColor enables color only when out is a real terminal - never when
// it's a file, a pipe, or (importantly) a bytes.Buffer in a test.
func shouldColor(out io.Writer) bool {
	f, ok := out.(*os.File)
	if !ok {
		return false
	}
	return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
}
