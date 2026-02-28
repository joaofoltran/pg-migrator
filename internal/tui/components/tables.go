package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/jfoltran/migrator/internal/metrics"
)

var (
	tblHeaderStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#3B82F6"))
	tblCopyingStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B"))
	tblCopiedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981"))
	tblStreamStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#3B82F6"))
	tblPendingStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
)

// RenderTables renders the per-table progress table.
func RenderTables(snap metrics.Snapshot, width, maxRows int) string {
	if len(snap.Tables) == 0 {
		return "  No table data available"
	}

	var b strings.Builder

	// Header.
	header := fmt.Sprintf("  %-35s %-18s %-10s %s", "Table", "Rows", "Size", "Progress")
	b.WriteString(tblHeaderStyle.Render(header))
	b.WriteByte('\n')

	shown := len(snap.Tables)
	if maxRows > 0 && shown > maxRows {
		shown = maxRows
	}

	for i := 0; i < shown; i++ {
		t := snap.Tables[i]
		name := t.Schema + "." + t.Name
		if len(name) > 33 {
			name = name[:30] + "..."
		}

		var rowsStr, progressStr string

		switch t.Status {
		case metrics.TableCopying:
			rowsStr = fmt.Sprintf("%s/%s", formatCount(t.RowsCopied), formatCount(t.RowsTotal))
			bar := miniBar(t.Percent, 12)
			progressStr = tblCopyingStyle.Render(fmt.Sprintf("%s %5.1f%%", bar, t.Percent))
		case metrics.TableCopied:
			rowsStr = fmt.Sprintf("%s/%s", formatCount(t.RowsCopied), formatCount(t.RowsTotal))
			bar := miniBar(100, 12)
			progressStr = tblCopiedStyle.Render(fmt.Sprintf("%s  100%%", bar))
		case metrics.TableStreaming:
			rowsStr = "STREAMING"
			progressStr = tblStreamStyle.Render("⟳ live")
		default:
			rowsStr = fmt.Sprintf("0/%s", formatCount(t.RowsTotal))
			bar := miniBar(0, 12)
			progressStr = tblPendingStyle.Render(fmt.Sprintf("%s    0%%", bar))
		}

		sizeStr := formatBytes(t.SizeBytes)

		line := fmt.Sprintf("  %-35s %-18s %-10s %s", name, rowsStr, sizeStr, progressStr)
		b.WriteString(line)
		if i < shown-1 {
			b.WriteByte('\n')
		}
	}

	if len(snap.Tables) > shown {
		b.WriteByte('\n')
		b.WriteString(fmt.Sprintf("  ... and %d more tables", len(snap.Tables)-shown))
	}

	return b.String()
}

func miniBar(pct float64, width int) string {
	filled := int(pct / 100 * float64(width))
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

func formatCount(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(n)/1e9)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1e3)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func formatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
