package progress

import (
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// defaultWidth is used when the terminal will not tell us how wide it is.
const defaultWidth = 80

// fd extracts a file descriptor from w, if it has one.
func fd(w io.Writer) (int, bool) {
	f, ok := w.(*os.File)
	if !ok {
		return 0, false
	}
	return int(f.Fd()), true
}

// isTerminal reports whether w is an interactive terminal, i.e. whether it is
// safe to draw escape sequences and repaint a line in place.
func isTerminal(w io.Writer) bool {
	d, ok := fd(w)
	return ok && term.IsTerminal(d)
}

// terminalWidth returns the width of w's terminal, falling back to a sane
// default for pipes and terminals that do not report a size.
func terminalWidth(w io.Writer) int {
	d, ok := fd(w)
	if !ok {
		return defaultWidth
	}
	width, _, err := term.GetSize(d)
	if err != nil || width < 20 {
		return defaultWidth
	}
	return width
}

// colorEnabled honours the NO_COLOR convention and the dumb terminal, so the
// rainbow can be turned off by anyone who would rather it were not there.
func colorEnabled() bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	if strings.EqualFold(os.Getenv("FK_COLOR"), "never") {
		return false
	}
	return os.Getenv("TERM") != "dumb"
}

func (b *Bar) hideCursor() { _, _ = io.WriteString(b.w, "\x1b[?25l") }
func (b *Bar) showCursor() { _, _ = io.WriteString(b.w, "\x1b[?25h") }
