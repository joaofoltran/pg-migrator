package components

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"

	"github.com/jfoltran/pgmigrator/internal/metrics"
)

var throughputValueStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF"))

// RenderThroughput renders the throughput counters.
func RenderThroughput(snap metrics.Snapshot, width int) string {
	rowsPerSec := throughputValueStyle.Render(fmt.Sprintf("%.0f rows/s", snap.RowsPerSec))
	bytesPerSec := throughputValueStyle.Render(formatBytes(int64(snap.BytesPerSec)) + "/s")
	totalRows := formatCount(snap.TotalRows)
	totalBytes := formatBytes(snap.TotalBytes)

	errStr := ""
	if snap.ErrorCount > 0 {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444"))
		errStr = fmt.Sprintf("  Errors: %s", errStyle.Render(fmt.Sprintf("%d", snap.ErrorCount)))
	}

	return fmt.Sprintf("  %s  |  %s  |  Total: %s rows, %s%s",
		rowsPerSec, bytesPerSec, totalRows, totalBytes, errStr)
}
