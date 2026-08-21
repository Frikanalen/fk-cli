// Package progress renders an animated, unapologetically rainbow-coloured
// progress bar for long-running operations like uploads and ingest jobs.
//
// The bar draws itself on a single line of a terminal, repainting on a timer
// so the colours keep shimmering even while progress stands still. When the
// output is not a terminal (a pipe, a log file, CI) it degrades to occasional
// plain-text lines instead, so build logs stay readable.
package progress

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// Units selects how Bar formats the current/total counters it is given.
type Units int

const (
	// UnitsNone prints no counters at all, only the percentage.
	UnitsNone Units = iota
	// UnitsBytes prints counters as human-readable byte sizes, plus a
	// transfer rate and an ETA.
	UnitsBytes
)

const (
	// frameInterval is how often the bar repaints; fast enough for the
	// gradient to flow, slow enough to be cheap.
	frameInterval = 60 * time.Millisecond
	// plainInterval is the minimum time between lines in non-terminal mode.
	plainInterval = 2 * time.Second
	// plainStep is the minimum percentage change between lines in
	// non-terminal mode.
	plainStep = 5
)

// spinnerFrames is a braille spinner: dense enough to look smooth at 60ms.
var spinnerFrames = []rune("⣾⣽⣻⢿⡿⣟⣯⣷")

// partials are the eighth-width block elements used to render the fraction of
// a cell that does not divide evenly, so the bar advances smoothly.
var partials = []rune("▏▎▍▌▋▊▉")

// Bar is a single-line animated progress indicator. Its methods are safe to
// call from multiple goroutines; Update in particular is meant to be called
// from an upload's progress callback while the render loop runs on its own
// timer.
type Bar struct {
	w io.Writer

	fancy bool // draw the animated, coloured, single-line version
	width int  // total terminal width

	mu    sync.Mutex
	label string
	// units is the counter format of the current phase; see Next.
	units   Units
	current int64
	total   int64
	began   time.Time // when the whole operation started
	start   time.Time // when the current phase started
	rate    float64   // smoothed bytes/second
	lastAt  time.Time
	lastVal int64
	// barWidth is the width the bar has settled on for this phase, kept so
	// later frames — the closing one in particular — do not resize it.
	barWidth int
	done     bool
	lastLine string // plain mode: suppress identical repeats

	plainAt      time.Time
	plainPercent int

	stop chan struct{}
	wg   sync.WaitGroup
}

// New creates a bar that renders to w and starts animating immediately. The
// caller must eventually call Finish or Fail exactly once to stop the render
// loop and leave the cursor on a fresh line.
func New(w io.Writer, label string, units Units) *Bar {
	b := newBar(w, label, units, isTerminal(w) && colorEnabled(), terminalWidth(w))
	b.startLoop()
	return b
}

// newBar builds a bar without starting it, with the presentation decision
// (animated vs. plain, and how wide) passed in so tests can pin it down.
func newBar(w io.Writer, label string, units Units, fancy bool, width int) *Bar {
	now := time.Now()
	return &Bar{
		w:            w,
		units:        units,
		label:        label,
		total:        -1,
		began:        now,
		start:        now,
		lastAt:       now,
		fancy:        fancy,
		width:        width,
		stop:         make(chan struct{}),
		plainAt:      now.Add(-plainInterval),
		plainPercent: -1,
	}
}

// startLoop begins animating, or announces the operation once in plain mode.
func (b *Bar) startLoop() {
	if b.fancy {
		b.hideCursor()
		b.wg.Add(1)
		go b.loop()
	} else {
		b.plainf("%s...", b.label)
	}
}

// SetLabel changes the text shown ahead of the bar, e.g. when an upload hands
// over to a server-side ingest stage.
func (b *Bar) SetLabel(label string) {
	b.mu.Lock()
	changed := b.label != label
	b.label = label
	b.mu.Unlock()

	if !b.fancy && changed {
		b.plainReset()
		b.plainf("%s...", label)
	}
}

// Next carries the bar on to the next phase of the same operation — an upload
// handing over to the server-side ingest that follows it, say. The bar stays
// on the line it is already drawing, but takes a new label, new units and a
// fresh set of counters, so the rate and ETA describe the new phase rather
// than being dragged down by the old one.
func (b *Bar) Next(label string, units Units) {
	b.mu.Lock()
	now := time.Now()
	b.label, b.units = label, units
	b.current, b.total = 0, -1
	b.rate, b.lastVal, b.lastAt = 0, 0, now
	// A new phase has a new label, so the bar may fairly take a new width.
	b.barWidth = 0
	b.start = now
	b.mu.Unlock()

	if !b.fancy {
		b.plainReset()
		b.plainf("%s...", label)
	}
}

// Update reports progress. A total of zero or less means the length is
// unknown, and the bar switches to an indeterminate sweep.
func (b *Bar) Update(current, total int64) {
	b.mu.Lock()
	b.current = current
	b.total = total
	b.observeRate(current)
	label, units, percent := b.label, b.units, b.percentLocked()
	b.mu.Unlock()

	if !b.fancy {
		b.plainProgress(label, units, percent, current, total)
	}
}

// Finish stops the animation and leaves a completed bar (or, in plain mode, a
// final line) behind, followed by msg.
func (b *Bar) Finish(msg string) { b.end(true, msg) }

// Fail stops the animation and leaves the bar behind in a failed state,
// followed by msg.
func (b *Bar) Fail(msg string) { b.end(false, msg) }

func (b *Bar) end(ok bool, msg string) {
	b.mu.Lock()
	if b.done {
		b.mu.Unlock()
		return
	}
	b.done = true
	b.mu.Unlock()

	if b.fancy {
		close(b.stop)
		b.wg.Wait()
		b.render(true, ok, msg)
		b.showCursor()
		_, _ = fmt.Fprintln(b.w)
		return
	}

	b.plainReset()
	if ok {
		b.plainf("%s", msg)
	} else {
		b.plainf("%s", msg)
	}
}

// loop repaints the bar until the bar is finished.
func (b *Bar) loop() {
	defer b.wg.Done()
	t := time.NewTicker(frameInterval)
	defer t.Stop()
	for {
		select {
		case <-b.stop:
			return
		case <-t.C:
			b.render(false, true, "")
		}
	}
}

// observeRate folds the latest reading into an exponentially smoothed
// transfer rate. The caller must hold b.mu.
func (b *Bar) observeRate(current int64) {
	now := time.Now()
	elapsed := now.Sub(b.lastAt).Seconds()
	if elapsed < 0.05 || current <= b.lastVal {
		return
	}
	instant := float64(current-b.lastVal) / elapsed
	if b.rate == 0 {
		b.rate = instant
	} else {
		b.rate = 0.7*b.rate + 0.3*instant
	}
	b.lastAt, b.lastVal = now, current
}

// percentLocked returns progress in percent, or -1 when indeterminate. The
// caller must hold b.mu.
func (b *Bar) percentLocked() int {
	if b.total <= 0 {
		return -1
	}
	p := int(float64(b.current) * 100 / float64(b.total))
	if p > 100 {
		p = 100
	}
	return p
}
