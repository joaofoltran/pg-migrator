package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/jfoltran/pgmigrator/internal/metrics"
)

var (
	logTimeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
	logINF       = lipgloss.NewStyle().Foreground(lipgloss.Color("#3B82F6"))
	logWRN       = lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B"))
	logERR       = lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444"))
	logDBG       = lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
)

// RenderLogs renders the last N log entries.
func RenderLogs(entries []metrics.LogEntry, maxLines int) string {
	if len(entries) == 0 {
		return "  No log entries yet"
	}

	start := 0
	if len(entries) > maxLines {
		start = len(entries) - maxLines
	}

	var b strings.Builder
	for i := start; i < len(entries); i++ {
		e := entries[i]
		ts := logTimeStyle.Render(e.Time.Format("15:04:05"))

		var lvl string
		switch e.Level {
		case "info":
			lvl = logINF.Render("INF")
		case "warn":
			lvl = logWRN.Render("WRN")
		case "error":
			lvl = logERR.Render("ERR")
		default:
			lvl = logDBG.Render("DBG")
		}

		line := fmt.Sprintf("  %s %s %s", ts, lvl, e.Message)
		b.WriteString(line)
		if i < len(entries)-1 {
			b.WriteByte('\n')
		}
	}

	return b.String()
}
