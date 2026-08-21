package progress

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

// bytes.Buffer is not a terminal, so New must pick the plain line mode.
func TestPlainModeEmitsLinesWithoutEscapes(t *testing.T) {
	var buf bytes.Buffer
	b := New(&buf, "Uploading lol.mp4", UnitsBytes)

	b.Update(0, 1000)
	// Plain lines are rate limited by time as well as by percentage, so
	// pretend the last line is old enough for each step to get through.
	for _, n := range []int64{200, 500, 1000} {
		b.plainReset()
		b.Update(n, 1000)
	}
	b.Finish("uploaded")

	out := buf.String()
	if strings.Contains(out, "\x1b") {
		t.Errorf("plain mode wrote escape sequences: %q", out)
	}
	for _, want := range []string{"Uploading lol.mp4...", "20%", "50%", "100%", "uploaded"} {
		if !strings.Contains(out, want) {
			t.Errorf("plain output missing %q, got:\n%s", want, out)
		}
	}
}

func TestPlainModeRateLimitsRepeatedUpdates(t *testing.T) {
	var buf bytes.Buffer
	b := New(&buf, "Uploading", UnitsBytes)
	for i := int64(0); i < 500; i++ {
		b.Update(i, 100000)
	}
	b.Finish("uploaded")

	if lines := strings.Count(buf.String(), "\n"); lines > 4 {
		t.Errorf("expected a handful of lines, got %d:\n%s", lines, buf.String())
	}
}

func TestFinishIsIdempotent(t *testing.T) {
	var buf bytes.Buffer
	b := New(&buf, "Uploading", UnitsNone)
	b.Finish("uploaded")
	b.Finish("uploaded")
	b.Fail("failed")

	if strings.Contains(buf.String(), "failed") {
		t.Errorf("bar kept reporting after it finished:\n%s", buf.String())
	}
}

func TestPercentClampsAndReportsIndeterminate(t *testing.T) {
	b := &Bar{total: -1}
	if got := b.percentLocked(); got != -1 {
		t.Errorf("unknown total should be indeterminate, got %d", got)
	}
	b.total, b.current = 100, 250
	if got := b.percentLocked(); got != 100 {
		t.Errorf("percent should clamp to 100, got %d", got)
	}
}

func TestFilledBarGrowsWithProgress(t *testing.T) {
	empty := strings.Count(filledBar(20, 0, 0, false), "█")
	half := strings.Count(filledBar(20, 0.5, 0, false), "█")
	full := strings.Count(filledBar(20, 1, 0, false), "█")

	if empty != 0 || half != 10 || full != 20 {
		t.Errorf("unexpected fill counts: empty=%d half=%d full=%d", empty, half, full)
	}
}

// The comet must stay inside the track at every point of its travel.
func TestSweepBarKeepsWidth(t *testing.T) {
	for phase := 0.0; phase < 2; phase += 0.05 {
		if got := len([]rune(stripANSI(sweepBar(24, phase)))); got != 24 {
			t.Fatalf("phase %.2f: width %d, want 24", phase, got)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	cases := map[int64]string{
		0:               "0 B",
		512:             "512 B",
		1024:            "1.0 KiB",
		1536:            "1.5 KiB",
		5 * 1024 * 1024: "5.0 MiB",
	}
	for in, want := range cases {
		if got := formatBytes(in); got != want {
			t.Errorf("formatBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatETAAndDuration(t *testing.T) {
	if got := formatETA(0, 100, 0); got != "--:--" {
		t.Errorf("no rate yet should be unknown, got %q", got)
	}
	if got := formatETA(50, 100, 10); got != "00:05" {
		t.Errorf("formatETA = %q, want 00:05", got)
	}
	if got := formatDuration(3*time.Hour + 4*time.Minute + 5*time.Second); got != "3:04:05" {
		t.Errorf("formatDuration = %q, want 3:04:05", got)
	}
}

// stripANSI removes SGR escapes so tests can measure printed width.
func stripANSI(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			i++
			continue
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String()
}

// syncBuffer is a bytes.Buffer that a test can read while the bar's render
// goroutine is still writing to it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *syncBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Len()
}

// newTestFancy returns an animated bar drawing into buf at a fixed width,
// without needing a real terminal.
func newTestFancy(buf *syncBuffer, label string, units Units, width int) *Bar {
	b := newBar(buf, label, units, true, width)
	b.startLoop()
	return b
}

func TestFancyFrameFitsTerminalWidth(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		var buf syncBuffer
		b := newTestFancy(&buf, "Uploading lol.mp4", UnitsBytes, width)
		b.Update(37*1024*1024, 120*1024*1024)
		time.Sleep(2 * frameInterval)
		b.Finish("uploaded")

		for _, frame := range strings.Split(buf.String(), "\r\x1b[2K") {
			visible := strings.TrimRight(stripANSI(frame), "\n")
			if len([]rune(visible)) > width {
				t.Errorf("width %d: frame is %d columns wide: %q", width, len([]rune(visible)), visible)
			}
		}
	}
}

func TestFancyFrameShowsProgressAndFinishes(t *testing.T) {
	var buf syncBuffer
	b := newTestFancy(&buf, "Uploading lol.mp4", UnitsBytes, 100)
	b.Update(30*1024*1024, 120*1024*1024)
	time.Sleep(2 * frameInterval)
	b.Finish("uploaded")

	out := stripANSI(buf.String())
	for _, want := range []string{"Uploading lol.mp4", "25%", "30.0/120.0 MiB", "ETA", "✔", "uploaded", "100%"} {
		if !strings.Contains(out, want) {
			t.Errorf("frame missing %q, got:\n%s", want, out)
		}
	}
	if !strings.Contains(buf.String(), "\x1b[38;2;") {
		t.Error("expected 24-bit colour escapes in the animated bar")
	}
	if !strings.HasSuffix(buf.String(), "\x1b[?25h\n") {
		t.Error("bar should restore the cursor and end the line when finished")
	}
}

func TestFancyFrameKeepsAnimatingWithoutUpdates(t *testing.T) {
	var buf syncBuffer
	b := newTestFancy(&buf, "transcoding", UnitsNone, 80)
	b.Update(0, -1) // indeterminate: no percentage from this stage
	time.Sleep(6 * frameInterval)
	b.Fail("ingest failed")

	frames := strings.Split(buf.String(), "\r\x1b[2K")
	if len(frames) < 4 {
		t.Fatalf("expected several repaints, got %d", len(frames))
	}
	if frames[1] == frames[2] && frames[2] == frames[3] {
		t.Error("consecutive frames are identical; the bar is not animating")
	}
	if !strings.Contains(stripANSI(buf.String()), "✘") {
		t.Error("a failed bar should be marked as failed")
	}
}

func TestFitDegradesGracefullyOnNarrowTerminals(t *testing.T) {
	details := []string{"37.0/120.0 MiB · 1.2 MiB/s · ETA 01:10", "37.0/120.0 MiB · 1.2 MiB/s", "37.0/120.0 MiB"}

	for _, width := range []int{20, 24, 30, 40, 60, 80, 200} {
		label, right, barWidth := fit(width, "Uploading lol.mp4", " 30%", details, 0)

		used := 2 + runeLen(label) + 1 + 2 + barWidth + 1 + 4
		if label == "" {
			used -= runeLen(label) + 1
		}
		if right != "" {
			used += 2 + runeLen(right)
		}
		if used > width {
			t.Errorf("width %d: layout needs %d columns (label %q, right %q, bar %d)", width, used, label, right, barWidth)
		}
		if barWidth < 1 {
			t.Errorf("width %d: bar collapsed to %d", width, barWidth)
		}
	}
}

func TestFancyFrameFitsVeryNarrowTerminal(t *testing.T) {
	var buf syncBuffer
	b := newTestFancy(&buf, "Uploading a-rather-long-file-name.mp4", UnitsBytes, 24)
	b.Update(1024, 4096)
	time.Sleep(2 * frameInterval)
	b.Finish("uploaded")

	for _, frame := range strings.Split(buf.String(), "\r\x1b[2K") {
		visible := strings.TrimRight(stripANSI(frame), "\n")
		if len([]rune(visible)) > 24 {
			t.Errorf("frame is %d columns wide: %q", len([]rune(visible)), visible)
		}
	}
}

func TestFormatBytesPairSharesTheLargerUnit(t *testing.T) {
	cases := []struct {
		current, total int64
		want           string
	}{
		{0, 120 * 1024 * 1024, "0.0/120.0 MiB"},
		{512, 1000, "512/1000 B"},
		{1536, 4096, "1.5/4.0 KiB"},
	}
	for _, c := range cases {
		if got := formatBytesPair(c.current, c.total); got != c.want {
			t.Errorf("formatBytesPair(%d, %d) = %q, want %q", c.current, c.total, got, c.want)
		}
	}
}

// The upload and the ingest that follows it share one bar, so nothing may end
// the line between the two phases.
func TestNextKeepsBothPhasesOnOneLine(t *testing.T) {
	var buf syncBuffer
	b := newTestFancy(&buf, "Uploading lol.mp4", UnitsBytes, 90)
	b.Update(120<<20, 120<<20)
	time.Sleep(2 * frameInterval)

	b.Next("transcoding", UnitsNone)
	b.Update(40, 100)
	time.Sleep(2 * frameInterval)
	b.Finish("ready to broadcast")

	out := buf.String()
	if n := strings.Count(out, "\n"); n != 1 {
		t.Errorf("expected exactly one newline (at the very end), got %d:\n%q", n, out)
	}
	if !strings.HasSuffix(out, "\n") {
		t.Error("the finished bar should end its line")
	}

	frames := strings.Split(out, "\r\x1b[2K")
	last := stripANSI(frames[len(frames)-1])
	if !strings.Contains(last, "ready to broadcast") || strings.Contains(last, "Uploading") {
		t.Errorf("final frame should show the ingest result alone, got %q", last)
	}
}

// Next starts the new phase's counters from scratch, so a fast upload cannot
// leave a nonsense rate or ETA behind on a slow ingest.
func TestNextResetsCountersAndRate(t *testing.T) {
	var buf syncBuffer
	b := newTestFancy(&buf, "Uploading", UnitsBytes, 90)
	b.Update(60<<20, 120<<20)
	time.Sleep(2 * frameInterval)
	b.Next("probing", UnitsNone)

	b.mu.Lock()
	current, total, rate := b.current, b.total, b.rate
	b.mu.Unlock()

	// Let any frame that was already being painted when Next landed finish,
	// so what follows is unambiguously the new phase.
	time.Sleep(2 * frameInterval)
	newPhase := buf.Len()
	time.Sleep(2 * frameInterval)
	b.Finish("done")

	if current != 0 || total != -1 || rate != 0 {
		t.Errorf("Next left state behind: current=%d total=%d rate=%f", current, total, rate)
	}
	if strings.Contains(stripANSI(buf.String()[newPhase:]), "MiB") {
		t.Error("byte counters leaked into a phase that has no byte counters")
	}
}

func TestNextAnnouncesNewPhaseInPlainMode(t *testing.T) {
	var buf bytes.Buffer
	b := New(&buf, "Uploading lol.mp4", UnitsBytes)
	b.Update(1024, 1024)
	b.Next("transcoding", UnitsNone)
	b.Update(50, 100)
	b.Finish("ready to broadcast")

	out := buf.String()
	for _, want := range []string{"Uploading lol.mp4...", "transcoding...", "ready to broadcast"} {
		if !strings.Contains(out, want) {
			t.Errorf("plain output missing %q, got:\n%s", want, out)
		}
	}
}

// A width the bar has already settled on survives the closing frame: the
// completion message must give way, not the bar.
func TestFitKeepsALockedBarWidth(t *testing.T) {
	details := []string{"ready to broadcast · 03:18", "ready to broadcast"}

	label, right, barWidth := fit(100, "lol.mp4", "100%", details, 48)
	if barWidth != 48 {
		t.Errorf("locked bar width changed to %d", barWidth)
	}
	if label != "lol.mp4" {
		t.Errorf("label was trimmed to %q with room to spare", label)
	}
	if right != "ready to broadcast · 03:18" {
		t.Errorf("right-hand detail = %q, want the full message", right)
	}

	// Less room: the elapsed time goes before the bar does.
	_, right, barWidth = fit(88, "lol.mp4", "100%", details, 48)
	if barWidth != 48 || right != "ready to broadcast" {
		t.Errorf("bar %d, detail %q; want the bar held at 48 and the message shortened", barWidth, right)
	}

	// On a line too cramped for both, the message is what shrinks.
	_, right, barWidth = fit(70, "lol.mp4", "100%", details, 48)
	if barWidth != 48 || right != "" {
		t.Errorf("cramped line: bar %d, detail %q; want the bar held at 48", barWidth, right)
	}
}

func TestBarWidthSurvivesTheClosingFrame(t *testing.T) {
	var buf syncBuffer
	b := newTestFancy(&buf, "transcoding", UnitsNone, 88)
	b.Update(72, 100)
	time.Sleep(2 * frameInterval)
	b.SetLabel("lol.mp4")
	b.Finish("ready to broadcast")

	var widths []int
	for _, frame := range strings.Split(buf.String(), "\r\x1b[2K") {
		visible := strings.TrimRight(stripANSI(frame), "\n")
		if !strings.Contains(visible, "▕") {
			continue
		}
		bar := visible[strings.Index(visible, "▕")+len("▕") : strings.Index(visible, "▏")]
		widths = append(widths, len([]rune(bar)))
	}

	if len(widths) < 2 {
		t.Fatalf("expected several frames, got %d", len(widths))
	}
	for _, w := range widths {
		if w != widths[0] {
			t.Errorf("bar width varied across frames: %v", widths)
			break
		}
	}
}
