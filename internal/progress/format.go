package progress

import (
	"fmt"
	"math"
	"time"
)

// scaleBytes picks a binary unit for n and returns the value in it. Callers
// that print two related sizes use the exponent to keep both in the same unit.
func scaleBytes(n int64) (float64, int) {
	const unit = 1024
	v, exp := float64(n), 0
	for v >= unit && exp < 4 {
		v /= unit
		exp++
	}
	return v, exp
}

// unitName names the binary unit for an exponent returned by scaleBytes.
func unitName(exp int) string {
	if exp == 0 {
		return "B"
	}
	return string("KMGTP"[exp-1]) + "iB"
}

// formatBytes renders n in binary units, with just enough precision to be
// readable at a glance.
func formatBytes(n int64) string {
	v, exp := scaleBytes(n)
	if exp == 0 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f %s", v, unitName(exp))
}

// formatBytesPair renders a progress counter as "30.0/120.0 MiB": both sides
// share the unit of the larger one, which is shorter and easier to compare
// than spelling out the unit twice.
func formatBytesPair(current, total int64) string {
	_, exp := scaleBytes(total)
	if exp == 0 {
		return fmt.Sprintf("%d/%d B", current, total)
	}
	div := math.Pow(1024, float64(exp))
	return fmt.Sprintf("%.1f/%.1f %s", float64(current)/div, float64(total)/div, unitName(exp))
}

// formatETA estimates the remaining time from the smoothed rate, or returns
// placeholder dashes while there is not enough to go on.
func formatETA(current, total int64, rate float64) string {
	if rate <= 0 || total <= 0 || current >= total {
		return "--:--"
	}
	return formatDuration(time.Duration(float64(total-current) / rate * float64(time.Second)))
}

// formatDuration renders d as mm:ss, or h:mm:ss once it passes an hour.
func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(math.Round(d.Seconds()))
	h, m, s := total/3600, total/60%60, total%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}
