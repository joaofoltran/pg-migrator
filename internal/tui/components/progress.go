package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/jfoltran/pgmanager/internal/metrics"
)

var (
	progressFullChar  = lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981")).Render("█")
	progressEmptyChar = lipgloss.NewStyle().Foreground(lipgloss.Color("#374151")).Render("░")
)

// RenderProgress renders the overall migration progress bar.
func RenderProgress(snap metrics.Snapshot, width int) string {
	total := snap.TablesTotal
	copied := snap.TablesCopied
	if total == 0 {
		return "  No tables to copy"
	}

	pct := float64(copied) / float64(total) * 100

	// Bar width = available width - label overhead.
	barWidth := width - 40
	if barWidth < 10 {
		barWidth = 10
	}

	filled := int(float64(barWidth) * pct / 100)
	if filled > barWidth {
		filled = barWidth
	}
	empty := barWidth - filled

	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)
	filledPart := lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981")).Render(bar[:filled*len("█")])
	emptyPart := lipgloss.NewStyle().Foreground(lipgloss.Color("#374151")).Render(bar[filled*len("█"):])
	_ = filledPart
	_ = emptyPart

	// Simple approach: just color the whole bar as a string.
	fullChars := strings.Repeat("█", filled)
	emptyChars := strings.Repeat("░", empty)

	coloredFull := lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981")).Render(fullChars)
	coloredEmpty := lipgloss.NewStyle().Foreground(lipgloss.Color("#374151")).Render(emptyChars)

	return fmt.Sprintf("  Overall: %s%s %5.1f%% (%d/%d tables)",
		coloredFull, coloredEmpty, pct, copied, total)
}
