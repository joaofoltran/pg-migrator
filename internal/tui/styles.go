package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Colors.
	colorPrimary   = lipgloss.Color("#7C3AED") // Purple
	colorSuccess   = lipgloss.Color("#10B981") // Green
	colorWarning   = lipgloss.Color("#F59E0B") // Amber
	colorDanger    = lipgloss.Color("#EF4444") // Red
	colorInfo      = lipgloss.Color("#3B82F6") // Blue
	colorMuted     = lipgloss.Color("#6B7280") // Gray
	colorBg        = lipgloss.Color("#1F2937") // Dark gray
	colorBorder    = lipgloss.Color("#374151") // Border gray
	colorHighlight = lipgloss.Color("#A78BFA") // Light purple

	// Styles.
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary).
			MarginBottom(1)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(colorPrimary).
			Padding(0, 1)

	phaseStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorHighlight)

	valueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF"))

	labelStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1)

	progressFullStyle = lipgloss.NewStyle().
				Foreground(colorSuccess)

	progressEmptyStyle = lipgloss.NewStyle().
				Foreground(colorMuted)

	tableHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorInfo).
				BorderBottom(true).
				BorderStyle(lipgloss.NormalBorder()).
				BorderForeground(colorBorder)

	statusCopyingStyle = lipgloss.NewStyle().
				Foreground(colorWarning)

	statusCopiedStyle = lipgloss.NewStyle().
				Foreground(colorSuccess)

	statusStreamingStyle = lipgloss.NewStyle().
				Foreground(colorInfo)

	statusPendingStyle = lipgloss.NewStyle().
				Foreground(colorMuted)

	logINFStyle = lipgloss.NewStyle().
			Foreground(colorInfo)

	logWRNStyle = lipgloss.NewStyle().
			Foreground(colorWarning)

	logERRStyle = lipgloss.NewStyle().
			Foreground(colorDanger)

	helpStyle = lipgloss.NewStyle().
			Foreground(colorMuted)
)
