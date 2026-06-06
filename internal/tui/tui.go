package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/verils/caigo/internal/llm"
	"github.com/verils/caigo/internal/message"
	"github.com/verils/caigo/internal/session"
	"github.com/verils/caigo/internal/tool"
)

var mainColor = lipgloss.Color("#4682FA")
var logoColor = mainColor
var inputBgColor = lipgloss.Color("236")

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
	Model             llm.Model
	Tools             []tool.Tool
	Session           session.Session
	ModelName         string
	ContextWindowSize int              // 0 = hide denominator
	ContextEstimator  ContextEstimator // nil = hide usage
}

// Model is the bubbletea llm for the chat TUI.
type Model struct {
	conversation []Entry
	vp           viewport.Model
	input        textinput.Model
	ready        bool

	llm           llm.Model
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

// New creates a TUI llm from the given config.
func New(cfg Config) Model {
	ti := textinput.New()
	ti.Prompt = "  > "
	ti.Placeholder = "" // We render placeholder ourselves for full background coverage
	ti.Focus()
	ti.CharLimit = 0
	return Model{
		llm:           cfg.Model,
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
	p := tea.NewProgram(m)
	_, err := p.Run()
	return err
}

// --- bubbletea.Model interface ---

func (m Model) Init() tea.Cmd {
	return textinput.Blink
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		headerH := lipgloss.Height(m.renderHeader())
		inputH := lipgloss.Height(m.renderInput())
		hintH := lipgloss.Height(m.renderHint())
		vpH := m.height - headerH - inputH - hintH
		if vpH < 1 {
			vpH = 1
		}

		m.vp.SetWidth(m.width)
		m.vp.SetHeight(vpH)
		m.vp.YPosition = headerH
		m.vp.FillHeight = true
		m.ready = true
		m.syncViewport()
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

func (m Model) View() tea.View {
	// Layout: header | [viewport] | input | hint
	header := m.renderHeader()
	input := m.renderInput()
	hint := m.renderHint()

	return tea.NewView(header + "\n" + m.vp.View() + "\n" + input + "\n" + hint)
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

	styleStatus = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Padding(0, 1)

	styleSpinner = lipgloss.NewStyle().
			Foreground(lipgloss.Color("243"))

	separator = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))
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

	var prefix, text string
	var style lipgloss.Style
	switch e.Kind {
	case EntryUser:
		prefix, style = "  > ", styleUser
		text = e.Content
	case EntryAssistant:
		prefix, style = "    ", styleAssistant
		text = e.Content
	case EntryThinking:
		prefix, style = "  💭 ", styleThinking
		text = e.Content
	case EntryToolCall:
		prefix, style = "  🔧 ", styleToolCall
		text = e.Content
	}

	innerW := w - 4
	if innerW < 10 {
		innerW = 10
	}
	fmt.Fprintln(b, style.Render(prefix)+style.Width(innerW).Render(text))
	fmt.Fprintln(b, separator.Render(strings.Repeat("─", w-4)))
}

func (m Model) renderHeader() string {
	art := []string{
		"",
		" ██████╗  █████╗ ██╗ ██████╗  ██████╗",
		"██╔════╝ ██╔══██╗██║██╔════╝ ██╔═══██╗",
		"██║      ███████║██║██║  ███╗██║   ██║",
		"██║      ██╔══██║██║██║   ██║██║   ██║",
		"╚██████╗ ██║  ██║██║╚██████╔╝╚██████╔╝",
		" ╚═════╝ ╚═╝  ╚═╝╚═╝ ╚═════╝  ╚═════╝",
		"     ░░░ Autonomous Agent v0.1.1 ░░░",
		"",
	}
	var lines []string
	for _, item := range art {
		lines = append(lines, "  "+lipgloss.NewStyle().Foreground(logoColor).Render(item))
	}
	header := strings.Join(lines, "\n")
	return lipgloss.JoinVertical(lipgloss.Left, header)
}

func (m Model) workDir() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	return dir
}

func (m Model) renderHint() string {
	if m.quitHint != "" && time.Now().Before(m.quitHintExpiry) {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Width(m.width).Render("  " + m.quitHint)
	}
	return lipgloss.NewStyle().Width(m.width).Render("")
}

func formatContextSize(tokens int) string {
	switch {
	case tokens >= 1_000_000:
		return fmt.Sprintf("%.0fM", float64(tokens)/1_000_000)
	case tokens >= 1_000:
		return fmt.Sprintf("%.0fK", float64(tokens)/1_000)
	default:
		return fmt.Sprintf("%d", tokens)
	}
}

func (m Model) inputHeight() int {
	if m.busy {
		return 1
	}
	lines := lipgloss.Height(m.input.View())
	if lines < 1 {
		lines = 1
	}
	return lines
}

func (m Model) renderInput() string {
	if m.busy {
		return lipgloss.NewStyle().Width(m.width).Render(
			styleSpinner.Render("  ⏳ Waiting for response..."))
	}
	if m.input.Value() == "" {
		// Render placeholder ourselves — textinput's placeholder uses
		// unstyled strings.Repeat(" ", n) for padding which shows as black.
		promptStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#007D9C")).Background(inputBgColor).Bold(true)
		prompt := promptStyle.Render("  > ")
		cursor := lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Background(inputBgColor).Render("█")
		ph := lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Background(inputBgColor).Render(" Type a message...")
		padW := m.width - lipgloss.Width(prompt+cursor+ph)
		if padW < 0 {
			padW = 0
		}
		pad := lipgloss.NewStyle().Background(inputBgColor).Width(padW).Render("")
		return m.wrapInputBlock(prompt + cursor + ph + pad)
	}
	// Non-empty input: render textinput view, apply matching background,
	// and pad each line to full width so the background fills uniformly.
	// Supports multi-line input (textinput.View() may contain newlines).
	bgStyle := lipgloss.NewStyle().Background(inputBgColor)
	lines := strings.Split(m.input.View(), "\n")
	for i, line := range lines {
		padW := m.width - lipgloss.Width(line)
		if padW < 0 {
			padW = 0
		}
		lines[i] = bgStyle.Render(line) + bgStyle.Render(strings.Repeat(" ", padW))
	}
	return m.wrapInputBlock(strings.Join(lines, "\n"))
}

// wrapInputBlock wraps an input block (one or more lines) with the
// top/bottom ▄/▀ borders in the input background color.
func (m Model) wrapInputBlock(block string) string {
	w := m.width
	if w <= 0 {
		w = 80
	}

	upBorder := lipgloss.NewStyle().
		Foreground(inputBgColor).
		Render(strings.Repeat("▄", w))
	downBorder := lipgloss.NewStyle().
		Foreground(inputBgColor).
		Render(strings.Repeat("▀", w))
	return lipgloss.JoinVertical(lipgloss.Left, upBorder, block, downBorder)
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
	// Each task gets its own channel; previous channel (if any) is abandoned.
	ch := make(chan tea.Msg, 100)
	m.eventCh = ch
	task := session.NewTask(m.llm, m.tools)
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
