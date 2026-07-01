package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

const (
	cardLeft  = 2
	cardRight = 2
)

// renderCard renders text inside a bordered region with left/right margins.
// Each output line is: leftPad + styledContent(innerW) + rightPad.
// firstPrefix overrides leftPad on the first line when non-empty.
func renderCard(w, left, right int, style lipgloss.Style, text, firstPrefix string) string {
	if w <= 0 {
		w = 80
	}
	innerW := w - left - right
	if innerW < 1 {
		innerW = 1
	}

	leftPad := strings.Repeat(" ", left)
	rightPad := strings.Repeat(" ", right)

	lines := strings.Split(style.Width(innerW).Render(text), "\n")
	for i, line := range lines {
		pad := leftPad
		if i == 0 && firstPrefix != "" {
			pad = firstPrefix
		}
		lines[i] = pad + line + rightPad
	}
	return strings.Join(lines, "\n")
}

func formatTokenCount(n int) string {
	if n >= 1000000 {
		return fmt.Sprintf("%.0fM", float64(n)/1000000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.0fK", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}
