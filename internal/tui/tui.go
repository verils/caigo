package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/verils/caigo/internal/message"
	"github.com/verils/caigo/internal/model"
	"github.com/verils/caigo/internal/session"
	"github.com/verils/caigo/internal/tool"
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

// ContextEstimator reports the current context token usage.
type ContextEstimator interface {
	ContextTokens() int
}

// Config holds TUI creation parameters.
type Config struct {
	Model             model.Model
	Tools             []tool.Tool
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

	model         model.Model
	tools         []tool.Tool
	sess          session.Session
	modelName     string
	ctxWindowSize int
	ctxEstimator  ContextEstimator

	busy      bool   // task is running
	streamBuf string // current assistant message being streamed
	thinkBuf  string // current thinking block being streamed
	width     int
	height    int

	cancelTask     context.CancelFunc // cancel current task
	eventCh        chan tea.Msg       // channel for task events
	lastQuitTime   time.Time          // last quit key press time
	quitHint       string             // quit hint message to display
	quitHintExpiry time.Time          // when to clear the quit hint
}

// New creates a TUI model from the given config.
func New(cfg Config) Model {
	ti := textinput.New()
	ti.Placeholder = "Type a message..."
	ti.Focus()
	ti.CharLimit = 0
	ti.Width = 80

	return Model{
		model:         cfg.Model,
		tools:         cfg.Tools,
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
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseAllMotion())
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
		case "ctrl+c", "ctrl+d":
			if m.busy {
				m.cancelCurrentTask()
				return m, nil
			}
			now := time.Now()
			if now.Sub(m.lastQuitTime) < 3*time.Second {
				return m, tea.Quit
			}
			m.lastQuitTime = now
			m.quitHint = "Press Ctrl+C again to quit"
			m.quitHintExpiry = now.Add(3 * time.Second)
			return m, m.tickHintClear()
		case "esc":
			if m.busy {
				m.cancelCurrentTask()
			}
			return m, nil
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
			return m, m.runTask(text)
		}

	case agentDeltaMsg:
		if msg.err != nil {
			if msg.err != context.Canceled {
				m.conversation = append(m.conversation, Entry{Kind: EntryAssistant, Content: "Error: " + msg.err.Error()})
			}
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
		// Mark the last tool call as completed
		for i := len(m.conversation) - 1; i >= 0; i-- {
			if m.conversation[i].Kind == EntryToolCall {
				m.conversation[i].Content += " ✓"
				break
			}
		}
		m.syncViewport()
		return m, m.nextEvent()

	case agentDoneMsg:
		m.flushStreamBuf()
		m.flushThinkBuf()
		m.busy = false
		m.syncViewport()
		return m, nil

	case tickMsg:
		if time.Now().After(m.quitHintExpiry) {
			m.quitHint = ""
		}
		return m, nil
	}

	// Always forward events to viewport for scrolling support.
	var vpCmd tea.Cmd
	m.vp, vpCmd = m.vp.Update(msg)

	if !m.busy {
		var inputCmd tea.Cmd
		m.input, inputCmd = m.input.Update(msg)
		return m, tea.Batch(vpCmd, inputCmd)
	}

	return m, vpCmd
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

func (m *Model) cancelCurrentTask() {
	if m.cancelTask != nil {
		m.cancelTask()
		m.cancelTask = nil
	}
	m.flushStreamBuf()
	m.flushThinkBuf()
	m.busy = false
	m.conversation = append(m.conversation, Entry{Kind: EntryAssistant, Content: "[Task cancelled]"})
	m.syncViewport()
}

// --- rendering ---

var (
	styleUser = lipgloss.NewStyle().
			Foreground(lipgloss.Color("39")).
			Bold(true).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("39")).
			Padding(0, 1)

	styleAssistant = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("245")).
			Padding(0, 1)

	styleThinking = lipgloss.NewStyle().
			Foreground(lipgloss.Color("243")).
			Italic(true).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(0, 1)

	styleToolCall = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Bold(true).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("214")).
			Padding(0, 1)

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
	// Card width: terminal width minus outer margins (4 chars each side)
	cardW := w - 8
	if cardW < 20 {
		cardW = 20
	}

	var card string
	switch e.Kind {
	case EntryUser:
		card = styleUser.Width(cardW).Render(e.Content)
	case EntryAssistant:
		card = styleAssistant.Width(cardW).Render(e.Content)
	case EntryThinking:
		card = styleThinking.Width(cardW).Render("💭 " + e.Content)
	case EntryToolCall:
		card = styleToolCall.Width(cardW).Render("🔧 " + e.Content)
	}
	// Indent card from left edge
	fmt.Fprintln(b, "    "+card)
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
	if m.quitHint != "" && time.Now().Before(m.quitHintExpiry) {
		bar += "· " + m.quitHint + " "
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

type tickMsg struct{}

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

type agentToolResultMsg struct{}

type agentDoneMsg struct{}

// runTask launches the task in a background goroutine and returns a tea.Cmd
// that blocks on the event channel until the first event arrives.
func (m *Model) runTask(input string) tea.Cmd {
	ch := m.eventCh
	task := session.NewTask(m.model, m.tools)
	sess := m.sess
	ctx, cancel := context.WithCancel(context.Background())
	m.cancelTask = cancel

	go func() {
		defer close(ch)

		if err := sess.Append(ctx, message.User(input)); err != nil {
			ch <- agentDeltaMsg{err: err}
			return
		}

		_, err := task.Run(ctx, sess, func(ev session.Event) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			switch ev.Type {
			case session.EventContentDelta:
				ch <- agentDeltaMsg{text: ev.Delta}
			case session.EventToolCall:
				if ev.ToolCall != nil {
					tc := ev.ToolCall
					ch <- agentToolCallMsg{text: fmt.Sprintf("%s(%s)", tc.Name, tc.Input)}
				}
			case session.EventToolResult:
				ch <- agentToolResultMsg{}
			}
			return nil
		})
		if err != nil {
			ch <- agentDeltaMsg{err: err}
			return
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

// tickHintClear returns a tea.Cmd that clears the quit hint after expiry.
func (m Model) tickHintClear() tea.Cmd {
	delay := time.Until(m.quitHintExpiry)
	if delay <= 0 {
		delay = 2 * time.Second
	}
	return tea.Tick(delay, func(t time.Time) tea.Msg {
		return tickMsg{}
	})
}
