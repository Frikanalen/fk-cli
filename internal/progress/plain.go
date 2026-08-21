package progress

import (
	"fmt"
	"time"
)

// plainProgress emits an occasional progress line for non-terminal output.
// Lines are rate limited by both time and percentage so that piping a long
// upload into a log does not produce thousands of them.
func (b *Bar) plainProgress(label string, units Units, percent int, current, total int64) {
	b.mu.Lock()
	stale := time.Since(b.plainAt) >= plainInterval
	stepped := percent < 0 || percent >= b.plainPercent+plainStep || percent == 100
	if !stale || !stepped {
		b.mu.Unlock()
		return
	}
	b.plainAt, b.plainPercent = time.Now(), percent
	b.mu.Unlock()

	switch {
	case units == UnitsBytes && total > 0:
		b.plainf("%s: %d%% (%s/%s)", label, percent, formatBytes(current), formatBytes(total))
	case units == UnitsBytes:
		b.plainf("%s: %s", label, formatBytes(current))
	case percent >= 0:
		b.plainf("%s: %d%%", label, percent)
	default:
		b.plainf("%s...", label)
	}
}

// plainReset forgets the rate-limiting state so the next line is emitted
// immediately, e.g. after the label changes.
func (b *Bar) plainReset() {
	b.mu.Lock()
	b.plainAt = time.Now().Add(-plainInterval)
	b.plainPercent = -1
	b.mu.Unlock()
}

// plainf writes one line, skipping it if it would repeat the previous one.
func (b *Bar) plainf(format string, args ...any) {
	line := fmt.Sprintf(format, args...)

	b.mu.Lock()
	repeat := line == b.lastLine
	b.lastLine = line
	b.mu.Unlock()

	if repeat {
		return
	}
	_, _ = fmt.Fprintln(b.w, line)
}
