package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jfoltran/pgmigrator/internal/metrics"
	"github.com/jfoltran/pgmigrator/internal/tui/components"
)

// snapshotMsg carries a new metrics snapshot into the Bubble Tea update loop.
type snapshotMsg metrics.Snapshot

// Model is the main Bubble Tea model for the pgmigrator TUI dashboard.
type Model struct {
	collector  *metrics.Collector
	sub        chan metrics.Snapshot
	snapshot   metrics.Snapshot
	lagHistory *components.LagHistory

	width  int
	height int
	ready  bool
}

// NewModel creates a new TUI model connected to the given metrics collector.
func NewModel(collector *metrics.Collector) Model {
	return Model{
		collector:  collector,
		lagHistory: components.NewLagHistory(60),
	}
}

// Init starts the subscription to metrics updates.
func (m Model) Init() tea.Cmd {
	m.sub = m.collector.Subscribe()
	return waitForSnapshot(m.sub)
}

func waitForSnapshot(sub chan metrics.Snapshot) tea.Cmd {
	return func() tea.Msg {
		snap, ok := <-sub
		if !ok {
			return nil
		}
		return snapshotMsg(snap)
	}
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			if m.sub != nil {
				m.collector.Unsubscribe(m.sub)
			}
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true

	case snapshotMsg:
		m.snapshot = metrics.Snapshot(msg)
		return m, waitForSnapshot(m.sub)
	}

	return m, nil
}

// View renders the full dashboard.
func (m Model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	w := m.width
	snap := m.snapshot

	var sections []string

	// Title bar.
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(colorPrimary).
		Width(w).
		Padding(0, 1).
		Render(" pgmigrator")
	sections = append(sections, title)

	// Header: phase, elapsed, lag, throughput.
	headerBox := boxStyle.Width(w - 2).Render(components.RenderHeader(snap, w-4))
	sections = append(sections, headerBox)

	// Overall progress.
	progressBox := boxStyle.Width(w - 2).Render(components.RenderProgress(snap, w-4))
	sections = append(sections, progressBox)

	// Table progress.
	tableHeight := m.height - 18 // Reserve space for other sections.
	if tableHeight < 3 {
		tableHeight = 3
	}
	tableContent := components.RenderTables(snap, w-4, tableHeight)
	tableBox := boxStyle.Width(w - 2).Render(tableContent)
	sections = append(sections, tableBox)

	// Lag sparkline.
	lagBox := boxStyle.Width(w - 2).Render(components.RenderLag(snap, m.lagHistory, w-4))
	sections = append(sections, lagBox)

	// Throughput.
	tpBox := boxStyle.Width(w - 2).Render(components.RenderThroughput(snap, w-4))
	sections = append(sections, tpBox)

	// Logs (last 5 lines).
	logEntries := m.collector.Logs()
	logBox := boxStyle.Width(w - 2).Render(components.RenderLogs(logEntries, 5))
	sections = append(sections, logBox)

	// Help.
	help := helpStyle.Render("  q: quit")
	sections = append(sections, help)

	return strings.Join(sections, "\n")
}

// Run starts the TUI in fullscreen mode.
func Run(collector *metrics.Collector) error {
	model := NewModel(collector)
	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err := p.Run()
	if err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}
