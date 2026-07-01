package tui

import (
	"time"

	"charm.land/lipgloss/v2"
)

// Hint displays a temporary quit hint.
type Hint struct {
	Width  int
	Text   string
	Expiry time.Time
}

func (h Hint) Render() string {
	if h.Text != "" && time.Now().Before(h.Expiry) {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Width(h.Width).Render("  "+h.Text) + "\n"
	}
	return ""
}
