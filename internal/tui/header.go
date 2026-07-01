package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

var logoColor = lipgloss.Color("#4682FA")

func renderHeader() string {
	art := []string{
		"",
		" ██████╗  █████╗ ██╗ ██████╗  ██████╗",
		"██╔════╝ ██╔══██╗██║██╔════╝ ██╔═══██╗",
		"██║      ███████║██║██║  ███╗██║   ██║",
		"██║      ██╔══██║██║██║   ██║██║   ██║",
		"╚██████╗ ██║  ██║██║╚██████╔╝╚██████╔╝",
		" ╚═════╝ ╚═╝  ╚═╝╚═╝ ╚═════╝ ╚═════╝",
		"     ░░░ Autonomous Agent v0.1.1 ░░░",
		"",
	}
	var lines []string
	for _, item := range art {
		lines = append(lines, "  "+lipgloss.NewStyle().Foreground(logoColor).Render(item))
	}
	header := strings.Join(lines, "\n")
	return lipgloss.JoinVertical(lipgloss.Left, header) + "\n"
}
