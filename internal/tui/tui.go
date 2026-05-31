package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/verils/caigo/internal/agent"
	"github.com/verils/caigo/internal/message"
	"github.com/verils/caigo/internal/session"
)

// EntryKind identifies the type of conversation entry.
type EntryKind int

const (
	EntryUser       EntryKind = iota // user input
	EntryAssistant                   // AI response text
	EntryThinking                    // AI reasoning / chain-of-thought
	EntryToolCall                    // tool invocation
	EntryToolResult                  // tool output
)

// Entry is a single item in the conversation history.
type Entry struct {
	Kind    EntryKind
	Content string
}

// ContextEstimator reports the current context token usage.
type ContextEstimator interface {
	ContextTokens() int
}

// Config holds TUI creation parameters.
type Config struct {
	Agent             *agent.Agent
	Session           session.Session
	ModelName         string
	ContextWindowSize int              // 0 = hide denominator
	ContextEstimator  ContextEstimator // nil = hide usage
}

// Model is the bubbletea model for the chat TUI.
type Model struct {
	conversation []Entry
	vp           viewport.Model
	input        textinput.Model
	ready        bool

	ag            *agent.Agent
	sess          session.Session
	modelName     string
	ctxWindowSize int
	ctxEstimator  ContextEstimator

	busy      bool   // agent is running
	streamBuf string // current assistant message being streamed
	thinkBuf  string // current thinking block being streamed
	width     int
	height    int

	eventCh chan tea.Msg // channel for agent events
}

// New creates a TUI model from the given config.
func New(cfg Config) Model {
	ti := textinput.New()
	ti.Placeholder = "Type a message..."
	ti.Focus()
	ti.CharLimit = 0
	ti.Width = 80

	return Model{
		ag:            cfg.Agent,
		sess:          cfg.Session,
		modelName:     cfg.ModelName,
		ctxWindowSize: cfg.ContextWindowSize,
		ctxEstimator:  cfg.ContextEstimator,
		input:         ti,
		eventCh:       make(chan tea.Msg, 100),
	}
}

// Run starts the TUI event loop.
func Run(cfg Config) error {
	m := New(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

// --- tea.Model interface ---

func (m Model) Init() tea.Cmd {
	return textinput.Blink
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if !m.ready {
			m.vp = viewport.New(msg.Width, msg.Height-3)
			m.vp.SetContent(m.renderConversation())
			m.input.Width = msg.Width - 4
			m.ready = true
		} else {
			m.vp.Width = msg.Width
			m.vp.Height = msg.Height - 3
			m.input.Width = msg.Width - 4
			m.vp.SetContent(m.renderConversation())
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "enter":
			text := strings.TrimSpace(m.input.Value())
			if text == "" || m.busy {
				break
			}
			m.input.SetValue("")
			m.conversation = append(m.conversation, Entry{Kind: EntryUser, Content: text})
			m.busy = true
			m.streamBuf = ""
			m.thinkBuf = ""
			m.syncViewport()
			return m, m.runAgent(text)
		}

	case agentDeltaMsg:
		if msg.err != nil {
			m.conversation = append(m.conversation, Entry{Kind: EntryAssistant, Content: "Error: " + msg.err.Error()})
			m.busy = false
			m.streamBuf = ""
			m.thinkBuf = ""
		} else {
			m.streamBuf += msg.text
		}
		m.syncViewport()
		return m, m.nextEvent()

	case agentThinkingMsg:
		if msg.text == "end" {
			if m.thinkBuf != "" {
				m.conversation = append(m.conversation, Entry{Kind: EntryThinking, Content: m.thinkBuf})
				m.thinkBuf = ""
			}
		} else {
			m.thinkBuf += msg.text
		}
		m.syncViewport()
		return m, m.nextEvent()

	case agentToolCallMsg:
		m.flushStreamBuf()
		m.conversation = append(m.conversation, Entry{Kind: EntryToolCall, Content: msg.text})
		m.syncViewport()
		return m, m.nextEvent()

	case agentToolResultMsg:
		m.conversation = append(m.conversation, Entry{Kind: EntryToolResult, Content: msg.text})
		m.syncViewport()
		return m, m.nextEvent()

	case agentDoneMsg:
		m.flushStreamBuf()
		m.flushThinkBuf()
		m.busy = false
		m.syncViewport()
		return m, nil
	}

	if !m.busy {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m Model) View() string {
	if !m.ready {
		return "Initializing..."
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		m.vp.View(),
		m.renderInput(),
		m.renderStatusBar(),
	)
}

// --- helpers ---

func (m *Model) syncViewport() {
	m.vp.SetContent(m.renderConversation())
	m.vp.GotoBottom()
}

func (m *Model) flushStreamBuf() {
	if m.streamBuf != "" {
		m.conversation = append(m.conversation, Entry{Kind: EntryAssistant, Content: m.streamBuf})
		m.streamBuf = ""
	}
}

func (m *Model) flushThinkBuf() {
	if m.thinkBuf != "" {
		m.conversation = append(m.conversation, Entry{Kind: EntryThinking, Content: m.thinkBuf})
		m.thinkBuf = ""
	}
}

// --- rendering ---

var (
	styleUser = lipgloss.NewStyle().
			Foreground(lipgloss.Color("39")).
			Bold(true)

	styleAssistant = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	styleThinking = lipgloss.NewStyle().
			Foreground(lipgloss.Color("243")).
			Italic(true)

	styleToolCall = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Bold(true)

	styleToolResult = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

	styleStatus = lipgloss.NewStyle().
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("236")).
			Padding(0, 1)

	styleInputPrompt = lipgloss.NewStyle().
				Foreground(lipgloss.Color("39")).
				Bold(true)

	styleSpinner = lipgloss.NewStyle().
			Foreground(lipgloss.Color("243"))
)

func (m Model) renderConversation() string {
	var b strings.Builder
	for _, e := range m.conversation {
		m.renderEntry(&b, e)
	}
	if m.streamBuf != "" {
		m.renderEntry(&b, Entry{Kind: EntryAssistant, Content: m.streamBuf + " ▌"})
	}
	if m.thinkBuf != "" {
		m.renderEntry(&b, Entry{Kind: EntryThinking, Content: m.thinkBuf + " ..."})
	}
	return b.String()
}

func (m Model) renderEntry(b *strings.Builder, e Entry) {
	w := m.width
	if w <= 0 {
		w = 80
	}
	innerW := w - 4
	if innerW < 10 {
		innerW = 10
	}

	switch e.Kind {
	case EntryUser:
		prefix := styleUser.Render("  > ")
		text := styleUser.Width(innerW).Render(e.Content)
		fmt.Fprintln(b, prefix+text)
	case EntryAssistant:
		prefix := styleAssistant.Render("    ")
		text := styleAssistant.Width(innerW).Render(e.Content)
		fmt.Fprintln(b, prefix+text)
	case EntryThinking:
		prefix := styleThinking.Render("  💭 ")
		text := styleThinking.Width(innerW).Render(e.Content)
		fmt.Fprintln(b, prefix+text)
	case EntryToolCall:
		prefix := styleToolCall.Render("  🔧 ")
		text := styleToolCall.Width(innerW).Render(e.Content)
		fmt.Fprintln(b, prefix+text)
	case EntryToolResult:
		prefix := styleToolResult.Render("  📄 ")
		text := styleToolResult.Width(innerW).Render(e.Content)
		fmt.Fprintln(b, prefix+text)
	}
	fmt.Fprintln(b)
}

func (m Model) renderStatusBar() string {
	bar := " " + m.modelName + " "
	if m.ctxEstimator != nil {
		used := m.ctxEstimator.ContextTokens()
		if m.ctxWindowSize > 0 {
			bar += fmt.Sprintf("· used %d / %d tokens ", used, m.ctxWindowSize)
		} else {
			bar += fmt.Sprintf("· used %d tokens ", used)
		}
	}
	return styleStatus.Width(m.width).Render(bar)
}

func (m Model) renderInput() string {
	if m.busy {
		return styleSpinner.Width(m.width).Render("  ⏳ Waiting for response...")
	}
	return styleInputPrompt.Render("  > ") + m.input.View()
}

// --- agent bridge ---

type agentDeltaMsg struct {
	text string
	err  error
}

type agentThinkingMsg struct {
	text string // "end" signals close of a thinking block
}

type agentToolCallMsg struct {
	text string
}

type agentToolResultMsg struct {
	text string
}

type agentDoneMsg struct{}

// runAgent launches the agent in a background goroutine and returns a tea.Cmd
// that blocks on the event channel until the first event arrives.
func (m Model) runAgent(input string) tea.Cmd {
	ch := m.eventCh
	ag := m.ag
	sess := m.sess
	go func() {
		defer close(ch)

		ctx := context.Background()
		if err := sess.Append(ctx, message.User(input)); err != nil {
			ch <- agentDeltaMsg{err: err}
			return
		}
		history, err := sess.Messages(ctx)
		if err != nil {
			ch <- agentDeltaMsg{err: err}
			return
		}

		added, err := ag.Run(ctx, history, func(ev agent.Event) error {
			switch ev.Type {
			case agent.EventContentDelta:
				ch <- agentDeltaMsg{text: ev.Delta}
			case agent.EventToolCall:
				if ev.ToolCall != nil {
					tc := ev.ToolCall
					ch <- agentToolCallMsg{text: fmt.Sprintf("%s(%s)", tc.Name, tc.Input)}
				}
			case agent.EventToolResult:
				if ev.ToolResult != nil {
					ch <- agentToolResultMsg{text: ev.ToolResult.Content}
				}
			}
			return nil
		})
		if err != nil {
			ch <- agentDeltaMsg{err: err}
			return
		}
		for _, msg := range added {
			if err := sess.Append(ctx, msg); err != nil {
				ch <- agentDeltaMsg{err: err}
				return
			}
		}
	}()
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return agentDoneMsg{}
		}
		return msg
	}
}

// nextEvent returns a tea.Cmd that reads the next event from the channel.
func (m Model) nextEvent() tea.Cmd {
	ch := m.eventCh
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return agentDoneMsg{}
		}
		return msg
	}
}
