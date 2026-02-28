package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/jfoltran/migrator/internal/metrics"
)

const sparklineChars = "▁▂▃▄▅▆▇█"

// LagHistory keeps a rolling window of lag values for sparkline rendering.
type LagHistory struct {
	values []uint64
	cap    int
}

// NewLagHistory creates a history buffer with the given capacity.
func NewLagHistory(cap int) *LagHistory {
	return &LagHistory{
		values: make([]uint64, 0, cap),
		cap:    cap,
	}
}

// Push adds a new lag value.
func (h *LagHistory) Push(lag uint64) {
	if len(h.values) >= h.cap {
		copy(h.values, h.values[1:])
		h.values = h.values[:len(h.values)-1]
	}
	h.values = append(h.values, lag)
}

// Sparkline returns a sparkline string representation.
func (h *LagHistory) Sparkline(width int) string {
	if len(h.values) == 0 {
		return strings.Repeat("▁", width)
	}

	// Use last `width` values.
	vals := h.values
	if len(vals) > width {
		vals = vals[len(vals)-width:]
	}

	var maxVal uint64
	for _, v := range vals {
		if v > maxVal {
			maxVal = v
		}
	}
	if maxVal == 0 {
		maxVal = 1
	}

	runes := []rune(sparklineChars)
	var b strings.Builder
	for _, v := range vals {
		idx := int(float64(v) / float64(maxVal) * float64(len(runes)-1))
		if idx >= len(runes) {
			idx = len(runes) - 1
		}
		b.WriteRune(runes[idx])
	}

	// Pad if needed.
	for b.Len() < width {
		b.WriteRune(runes[0])
	}

	return b.String()
}

// RenderLag renders the lag display with sparkline.
func RenderLag(snap metrics.Snapshot, history *LagHistory, width int) string {
	history.Push(snap.LagBytes)

	lagColor := lipgloss.Color("#10B981") // green
	if snap.LagBytes > 10<<20 {
		lagColor = lipgloss.Color("#EF4444") // red
	} else if snap.LagBytes > 1<<20 {
		lagColor = lipgloss.Color("#F59E0B") // amber
	}

	lagStyle := lipgloss.NewStyle().Foreground(lagColor)

	sparkWidth := width - 30
	if sparkWidth < 10 {
		sparkWidth = 10
	}

	spark := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Render(history.Sparkline(sparkWidth))

	return fmt.Sprintf("  Lag: %s  %s",
		lagStyle.Render(snap.LagFormatted),
		spark)
}
