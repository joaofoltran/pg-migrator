package components

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/jfoltran/migrator/internal/metrics"
)

var (
	headerPhaseStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#A78BFA"))
	headerLabelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
	headerValueStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF"))
)

// RenderHeader renders the top status bar with phase, elapsed, lag, throughput.
func RenderHeader(snap metrics.Snapshot, width int) string {
	phase := headerPhaseStyle.Render(strings.ToUpper(snap.Phase))
	elapsed := formatDuration(snap.ElapsedSec)

	left := fmt.Sprintf("  Phase: %s    Elapsed: %s",
		phase,
		headerValueStyle.Render(elapsed))

	lag := headerValueStyle.Render(snap.LagFormatted)
	throughput := headerValueStyle.Render(fmt.Sprintf("%.0f rows/s", snap.RowsPerSec))

	right := fmt.Sprintf("Lag: %s    Throughput: %s  ",
		lag, throughput)

	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}

	return left + strings.Repeat(" ", gap) + right
}

func formatDuration(seconds float64) string {
	d := time.Duration(seconds * float64(time.Second))
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %02dm %02ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %02ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}
