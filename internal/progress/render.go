package progress

import (
	"fmt"
	"math"
	"strings"
	"time"
)

const (
	minBarWidth = 10
	// preferredBarWidth is the bar width worth defending: detail is dropped
	// from the right-hand side before the bar is squeezed below it.
	preferredBarWidth = 20
	maxBarWidth       = 48
	// hueSpan is how much of the colour wheel one full bar covers. Slightly
	// less than a full turn so the two ends do not meet in the same red.
	hueSpan = 0.85
	// hueDrift is how many turns around the colour wheel the gradient makes
	// per second; this is what makes the bar shimmer while it fills.
	hueDrift = 0.35
)

// render paints one frame. When final is true it draws a completed (or
// failed) bar and appends msg instead of the live counters.
func (b *Bar) render(final, ok bool, msg string) {
	b.mu.Lock()
	label, units, current, total := b.label, b.units, b.current, b.total
	percent, rate := b.percentLocked(), b.rate
	elapsed, overall := time.Since(b.start), time.Since(b.began)
	b.mu.Unlock()

	if final {
		percent = 100
		if !ok && total > 0 && current < total {
			percent = int(float64(current) * 100 / float64(total))
		}
	}

	phase := float64(time.Now().UnixNano()) / 1e9 * hueDrift

	// Detail candidates, richest first: a narrow terminal gets progressively
	// less of the right-hand side rather than a wrapped, jittering line.
	var details []string
	switch {
	case final:
		// The closing line reports the whole operation's duration, not just
		// that of the phase that happened to finish last.
		details = []string{fmt.Sprintf("%s · %s", msg, formatDuration(overall)), msg}
	case units == UnitsBytes && total > 0:
		details = []string{
			fmt.Sprintf("%s · %s/s · ETA %s", formatBytesPair(current, total),
				formatBytes(int64(rate)), formatETA(current, total, rate)),
			fmt.Sprintf("%s · %s/s", formatBytesPair(current, total), formatBytes(int64(rate))),
			formatBytesPair(current, total),
		}
	case units == UnitsBytes:
		details = []string{fmt.Sprintf("%s · %s/s", formatBytes(current), formatBytes(int64(rate))), formatBytes(current)}
	default:
		details = []string{formatDuration(elapsed)}
	}

	pct := "  --%"
	if percent >= 0 {
		pct = fmt.Sprintf("%3d%%", percent)
	}

	// The bar keeps the width it settled on: a phase ending, or a longer
	// detail string appearing, must not make it shrink mid-operation.
	b.mu.Lock()
	locked := b.barWidth
	b.mu.Unlock()

	label, right, barWidth := fit(b.width, label, pct, details, locked)

	if !final {
		b.mu.Lock()
		b.barWidth = barWidth
		b.mu.Unlock()
	}

	var line strings.Builder
	line.WriteString("\r\x1b[2K")

	switch {
	case final && ok:
		line.WriteString(colorize("✔", 0.33, 0.9, 1))
	case final:
		line.WriteString(colorize("✘", 0.0, 0.9, 1))
	default:
		frame := spinnerFrames[int(time.Now().UnixNano()/int64(frameInterval))%len(spinnerFrames)]
		line.WriteString(colorize(string(frame), math.Mod(phase*2, 1), 0.8, 1))
	}
	line.WriteByte(' ')

	if label != "" {
		line.WriteString(gradientText(label, phase))
		line.WriteByte(' ')
	}

	line.WriteString(dim("▕"))
	if percent < 0 && !final {
		line.WriteString(sweepBar(barWidth, phase))
	} else {
		line.WriteString(filledBar(barWidth, float64(percent)/100, phase, final && !ok))
	}
	line.WriteString(dim("▏"))

	line.WriteByte(' ')
	line.WriteString(bold(gradientText(pct, phase+0.5)))
	if right != "" {
		line.WriteString("  ")
		line.WriteString(dim(right))
	}

	_, _ = fmt.Fprint(b.w, line.String())
}

// filledBar draws a proportional bar: the filled part is a flowing rainbow
// with an eighth-of-a-cell partial edge, the rest is a dim track.
func filledBar(width int, frac, phase float64, failed bool) string {
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	exact := frac * float64(width)
	full := int(exact)
	rest := exact - float64(full)

	var s strings.Builder
	for i := 0; i < full; i++ {
		h := math.Mod(float64(i)/float64(width)*hueSpan+phase, 1)
		if failed {
			s.WriteString(colorize("█", 0, 0.75, 0.55))
			continue
		}
		s.WriteString(colorize("█", h, 0.95, 1))
	}
	if full < width && rest > 0.05 {
		h := math.Mod(float64(full)/float64(width)*hueSpan+phase, 1)
		idx := int(rest * float64(len(partials)))
		if idx >= len(partials) {
			idx = len(partials) - 1
		}
		s.WriteString(colorize(string(partials[idx]), h, 0.95, 1))
		full++
	}
	for i := full; i < width; i++ {
		s.WriteString(dim("·"))
	}
	return s.String()
}

// sweepBar draws the indeterminate variant: a rainbow comet bouncing back and
// forth along the track, for stages that report no percentage.
func sweepBar(width int, phase float64) string {
	comet := width / 3
	if comet < 3 {
		comet = 3
	}
	// Triangle wave over the travel range, so the comet reverses at the ends.
	span := float64(width - comet)
	pos := math.Abs(math.Mod(phase*3, 2)-1) * span

	var s strings.Builder
	for i := 0; i < width; i++ {
		d := math.Abs(float64(i) - pos - float64(comet)/2)
		if d > float64(comet)/2 {
			s.WriteString(dim("·"))
			continue
		}
		// Brightest at the comet's centre, fading towards its tails.
		v := 1 - d/(float64(comet)/2)*0.75
		h := math.Mod(float64(i)/float64(width)*hueSpan+phase, 1)
		s.WriteString(colorize("█", h, 0.95, v))
	}
	return s.String()
}

// gradientText paints each rune of s with its own hue, continuing the same
// wheel the bar uses so label and bar shimmer in step.
func gradientText(s string, phase float64) string {
	runes := []rune(s)
	var out strings.Builder
	for i, r := range runes {
		if r == ' ' {
			out.WriteRune(r)
			continue
		}
		h := math.Mod(float64(i)/float64(max(len(runes), 1))*hueSpan+phase, 1)
		out.WriteString(colorize(string(r), h, 0.75, 1))
	}
	return out.String()
}

// hsv converts h, s, v in [0,1] to 8-bit RGB.
func hsv(h, s, v float64) (int, int, int) {
	h = math.Mod(math.Mod(h, 1)+1, 1) * 6
	i := math.Floor(h)
	f := h - i
	p := v * (1 - s)
	q := v * (1 - s*f)
	t := v * (1 - s*(1-f))

	var r, g, bl float64
	switch int(i) % 6 {
	case 0:
		r, g, bl = v, t, p
	case 1:
		r, g, bl = q, v, p
	case 2:
		r, g, bl = p, v, t
	case 3:
		r, g, bl = p, q, v
	case 4:
		r, g, bl = t, p, v
	case 5:
		r, g, bl = v, p, q
	}
	return int(r*255 + 0.5), int(g*255 + 0.5), int(bl*255 + 0.5)
}

// colorize wraps s in a 24-bit foreground colour given as HSV, restoring the
// default foreground afterwards (rather than resetting every attribute, which
// would also clear any surrounding bold).
func colorize(s string, h, sat, val float64) string {
	r, g, b := hsv(h, sat, val)
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s\x1b[39m", r, g, b, s)
}

func dim(s string) string  { return "\x1b[2m" + s + "\x1b[22m" }
func bold(s string) string { return "\x1b[1m" + s + "\x1b[22m" }

func runeLen(s string) int { return len([]rune(s)) }

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// fit lays the line out for a terminal that is total columns wide. It picks
// the richest detail string that still leaves room for a usable bar, and
// shortens the label as a last resort, returning the label, detail and bar
// width to draw.
func fit(total int, label, pct string, details []string, locked int) (string, string, int) {
	// Spinner and its space, the space after the label, both bar brackets,
	// the space before the percentage and the percentage itself.
	overhead := 2 + 1 + 2 + 1 + runeLen(pct)
	room := func(detail string) int {
		cost := 0
		if detail != "" {
			cost = 2 + runeLen(detail)
		}
		return total - overhead - runeLen(label) - cost
	}

	// A width the bar has already been drawn at is honoured before anything
	// else, so that detail is what gives way when the line gets crowded.
	if locked > 0 {
		for _, detail := range append(details, "") {
			if room(detail) >= locked {
				return label, detail, locked
			}
		}
	}

	// The bar is the point of the exercise, so drop detail to keep it a
	// decent size first, and only then let it shrink towards its minimum.
	for _, floor := range []int{preferredBarWidth, minBarWidth} {
		for _, detail := range append(details, "") {
			if width := room(detail); width >= floor {
				return label, detail, min(width, maxBarWidth)
			}
		}
	}

	// Nothing fits: keep the bar at its minimum and trim the label to suit.
	labelRoom := total - overhead - minBarWidth
	if labelRoom < 4 {
		return "", "", max(total-overhead, 1)
	}
	return truncate(label, labelRoom), "", minBarWidth
}

// truncate shortens s to at most width runes, marking the cut with an
// ellipsis.
func truncate(s string, width int) string {
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	return string(r[:width-1]) + "…"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
