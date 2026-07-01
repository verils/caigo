package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// EntryKind identifies the type of conversation entry.
type EntryKind int

const (
	EntryUser      EntryKind = iota // user input
	EntryAssistant                  // AI response text
	EntryThinking                   // AI reasoning / chain-of-thought
	EntryToolCall                   // tool invocation
)

// Entry is a single item in the conversation history.
type Entry struct {
	Kind    EntryKind
	Content string
}

var (
	styleUser = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#4682FA")).
			Bold(true)

	styleAssistant = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	styleThinking = lipgloss.NewStyle().
			Foreground(lipgloss.Color("243")).
			Italic(true)

	styleToolCall = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Bold(true)
)

// Conversation renders a list of conversation entries and an optional streaming buffer.
type Conversation struct {
	Width     int
	Entries   []Entry
	StreamBuf string
}

func (c Conversation) Render() string {
	var b strings.Builder
	for _, e := range c.Entries {
		fmt.Fprintln(&b, c.renderEntry(e))
	}
	if c.StreamBuf != "" {
		fmt.Fprintln(&b, c.renderStreamBuf())
	}
	return b.String()
}

func (c Conversation) renderEntry(e Entry) string {
	var prefix string
	var style lipgloss.Style
	switch e.Kind {
	case EntryUser:
		prefix, style = "  > ", styleUser
	case EntryAssistant:
		prefix, style = "    ", styleAssistant
	case EntryThinking:
		prefix, style = "  💭 ", styleThinking
	case EntryToolCall:
		prefix, style = "  🔧 ", styleToolCall
	}
	return renderCard(c.Width, cardLeft, cardRight, style, e.Content, prefix)
}

func (c Conversation) renderStreamBuf() string {
	return renderCard(c.Width, cardLeft, cardRight, styleAssistant, c.StreamBuf, "    ")
}
