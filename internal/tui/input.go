package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

var inputBgColor = lipgloss.Color("236")

// InputArea renders the text input with border styling.
type InputArea struct {
	Width     int
	InputView string
}

func (ia InputArea) Render() string {
	bgStyle := lipgloss.NewStyle().Background(inputBgColor)

	lines := strings.Split(ia.InputView, "\n")
	for i, line := range lines {
		padW := ia.Width - lipgloss.Width(line)
		if padW < 0 {
			padW = 0
		}
		if padW > 0 {
			lines[i] = line + bgStyle.Render(strings.Repeat(" ", padW))
		} else {
			lines[i] = line
		}
	}
	return ia.wrapBlock(strings.Join(lines, "\n"))
}

func (ia InputArea) wrapBlock(block string) string {
	w := ia.Width
	if w <= 0 {
		w = 80
	}

	upBorder := lipgloss.NewStyle().
		Foreground(inputBgColor).
		Render(strings.Repeat("▄", w))
	downBorder := lipgloss.NewStyle().
		Foreground(inputBgColor).
		Render(strings.Repeat("▀", w))
	return lipgloss.JoinVertical(lipgloss.Left, upBorder, block, downBorder) + "\n"
}
