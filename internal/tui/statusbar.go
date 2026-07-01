package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// ContextEstimator reports the current context token usage.
type ContextEstimator interface {
	ContextTokens() int
}

// StatusBar displays model name and context usage.
type StatusBar struct {
	Width         int
	ModelName     string
	CtxWindowSize int
	CtxEstimator  ContextEstimator
}

func (sb StatusBar) Render() string {
	if sb.Width <= 0 {
		return ""
	}

	modelName := sb.ModelName
	if modelName == "" {
		modelName = "Unknown"
	}

	parts := []string{modelName}

	if sb.CtxWindowSize > 0 {
		parts = append(parts, fmt.Sprintf("%s Context", formatTokenCount(sb.CtxWindowSize)))
	}

	if sb.CtxEstimator != nil && sb.CtxWindowSize > 0 {
		used := sb.CtxEstimator.ContextTokens()
		percent := float64(used) / float64(sb.CtxWindowSize) * 100
		parts = append(parts, fmt.Sprintf("Used %.1f%% context", percent))
	}

	statusText := strings.Join(parts, " · ")

	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("243")).
		Width(sb.Width).
		PaddingLeft(2)
	return style.Render(statusText) + "\n"
}
