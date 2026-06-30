package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
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

// Model is the bubbletea model for the chat TUI.
type Model struct {
	conversation []Entry
	input        textarea.Model
	focusCmd     tea.Cmd
	viewport     viewport.Model

	llm           llm.Model
	tools         []tool.Tool
	sess          session.Session
	modelName     string
	ctxWindowSize int
	ctxEstimator  ContextEstimator

	busy      bool   // task is running
	streamBuf string // current assistant message being streamed
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
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.Prompt = " > "
	ta.Placeholder = "Type a message..."
	ta.DynamicHeight = true
	ta.MinHeight = 1
	ta.MaxHeight = 10
	ta.SetPromptFunc(3, func(info textarea.PromptInfo) string {
		if info.LineNumber == 0 {
			return " > "
		}
		return "   "
	})
	styles := ta.Styles()
	styles.Focused.Base = styles.Focused.Base.Background(inputBgColor)
	styles.Focused.Placeholder = styles.Focused.Placeholder.Background(inputBgColor)
	styles.Focused.Text = styles.Focused.Text.Background(inputBgColor)
	styles.Focused.CursorLine = styles.Focused.CursorLine.Background(inputBgColor)
	styles.Focused.Prompt = styles.Focused.Prompt.Background(inputBgColor)
	styles.Blurred.Base = styles.Blurred.Base.Background(inputBgColor)
	styles.Blurred.Placeholder = styles.Blurred.Placeholder.Background(inputBgColor)
	styles.Blurred.Text = styles.Blurred.Text.Background(inputBgColor)
	styles.Blurred.Prompt = styles.Blurred.Prompt.Background(inputBgColor)
	ta.SetStyles(styles)
	focusCmd := ta.Focus()
	ta.KeyMap.InsertNewline = key.NewBinding(
		key.WithKeys("shift+enter"),
		key.WithHelp("shift+enter", "newline"),
	)

	vp := viewport.New()
	vp.SoftWrap = true

	return Model{
		llm:           cfg.Model,
		tools:         cfg.Tools,
		sess:          cfg.Session,
		modelName:     cfg.ModelName,
		ctxWindowSize: cfg.ContextWindowSize,
		ctxEstimator:  cfg.ContextEstimator,
		input:         ta,
		focusCmd:      focusCmd,
		viewport:      vp,
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
	return m.focusCmd
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.SetWidth(msg.Width)
		m.viewport.SetHeight(msg.Height)
		m.input.SetWidth(msg.Width)
		return m, m.updateContent()

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "ctrl+d":
			if m.busy {
				m.cancelCurrentTask()
				return m, m.updateContent()
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
				return m, m.updateContent()
			}
			return m, nil
		case "enter":
			text := strings.TrimSpace(m.input.Value())
			if text == "" || m.busy {
				break
			}
			m.input.Reset()
			m.conversation = append(m.conversation, Entry{Kind: EntryUser, Content: text})
			m.busy = true
			m.streamBuf = ""
			taskCmd := m.runTask(text)
			return m, tea.Batch(m.updateContent(), taskCmd)
		case "pageup":
			m.viewport.ScrollUp(1)
			return m, nil
		case "pagedown":
			m.viewport.ScrollDown(1)
			return m, nil
		}

	case agentDeltaMsg:
		if msg.err != nil {
			if msg.err != context.Canceled {
				m.conversation = append(m.conversation, Entry{Kind: EntryAssistant, Content: "Error: " + msg.err.Error()})
			}
			m.busy = false
			m.streamBuf = ""
			return m, tea.Batch(m.updateContent(), m.nextEvent())
		}
		m.streamBuf += msg.text
		return m, tea.Batch(m.updateContent(), m.nextEvent())

	case agentToolCallMsg:
		m.flushStreamBuf()
		m.conversation = append(m.conversation, Entry{Kind: EntryToolCall, Content: msg.text})
		return m, tea.Batch(m.updateContent(), m.nextEvent())

	case agentToolResultMsg:
		for i := len(m.conversation) - 1; i >= 0; i-- {
			if m.conversation[i].Kind == EntryToolCall {
				m.conversation[i].Content += " ✓"
				break
			}
		}
		return m, tea.Batch(m.updateContent(), m.nextEvent())

	case agentDoneMsg:
		m.flushStreamBuf()
		m.busy = false
		return m, m.updateContent()

	case tickMsg:
		if time.Now().After(m.quitHintExpiry) {
			m.quitHint = ""
		}
		return m, nil
	}

	// When not busy, forward input events to textinput.
	if !m.busy {
		var inputCmd tea.Cmd
		m.input, inputCmd = m.input.Update(msg)
		return m, tea.Batch(inputCmd, m.updateContent())
	}

	return m, nil
}

func (m Model) View() tea.View {
	v := tea.NewView(m.viewport.View())
	v.AltScreen = true
	return v
}

// --- helpers ---

// updateContent rebuilds the full page content and sets it on the viewport,
// then scrolls to bottom.
func (m *Model) updateContent() tea.Cmd {
	var b strings.Builder

	// Header
	b.WriteString(m.renderHeader())

	// Conversation entries
	for _, e := range m.conversation {
		m.renderEntry(&b, e)
	}

	// Streaming buffer (in-progress assistant message)
	if m.streamBuf != "" {
		m.renderStreamBuf(&b)
	}

	// Input area
	b.WriteString(m.renderInput())

	// Status bar
	b.WriteString(m.renderStatusBar())

	// Quit hint
	b.WriteString(m.renderHint())

	m.viewport.SetContent(b.String())
	m.viewport.GotoBottom()
	return nil
}

func (m *Model) flushStreamBuf() {
	if m.streamBuf != "" {
		m.conversation = append(m.conversation, Entry{Kind: EntryAssistant, Content: m.streamBuf})
		m.streamBuf = ""
	}
}

func (m *Model) cancelCurrentTask() {
	if m.cancelTask != nil {
		m.cancelTask()
		m.cancelTask = nil
	}
	m.flushStreamBuf()
	m.busy = false
	m.conversation = append(m.conversation, Entry{Kind: EntryAssistant, Content: "[Task cancelled]"})
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

	cardLeft  = 2
	cardRight = 2
)

func (m Model) renderEntry(b *strings.Builder, e Entry) {
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

	card := renderCard(m.width, cardLeft, cardRight, style, e.Content, prefix)
	fmt.Fprintln(b, card)
}

func (m Model) renderStreamBuf(b *strings.Builder) {
	card := renderCard(m.width, cardLeft, cardRight, styleAssistant, m.streamBuf, "    ")
	fmt.Fprintln(b, card)
}

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
	return lipgloss.JoinVertical(lipgloss.Left, header) + "\n"
}

func (m Model) renderHint() string {
	if m.quitHint != "" && time.Now().Before(m.quitHintExpiry) {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Width(m.width).Render("  "+m.quitHint) + "\n"
	}
	return ""
}

func (m Model) renderStatusBar() string {
	if m.width <= 0 {
		return ""
	}

	modelName := m.modelName
	if modelName == "" {
		modelName = "Unknown"
	}

	parts := []string{modelName}

	if m.ctxWindowSize > 0 {
		parts = append(parts, fmt.Sprintf("%s Context", formatTokenCount(m.ctxWindowSize)))
	}

	if m.ctxEstimator != nil && m.ctxWindowSize > 0 {
		used := m.ctxEstimator.ContextTokens()
		percent := float64(used) / float64(m.ctxWindowSize) * 100
		parts = append(parts, fmt.Sprintf("Used %.1f%% context", percent))
	}

	statusText := strings.Join(parts, " · ")

	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("243")).
		Width(m.width).
		PaddingLeft(2)
	return style.Render(statusText) + "\n"
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

func (m Model) renderInput() string {
	bgStyle := lipgloss.NewStyle().Background(inputBgColor)

	view := m.input.View()
	lines := strings.Split(view, "\n")
	for i, line := range lines {
		padW := m.width - lipgloss.Width(line)
		if padW < 0 {
			padW = 0
		}
		if padW > 0 {
			lines[i] = line + bgStyle.Render(strings.Repeat(" ", padW))
		} else {
			lines[i] = line
		}
	}
	return m.wrapInputBlock(strings.Join(lines, "\n"))
}

// wrapInputBlock wraps an input block with top/bottom ▄/▀ borders.
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
	return lipgloss.JoinVertical(lipgloss.Left, upBorder, block, downBorder) + "\n"
}

// --- agent bridge ---

type tickMsg struct{}

type agentDeltaMsg struct {
	text string
	err  error
}

type agentToolCallMsg struct {
	text string
}

type agentToolResultMsg struct{}

type agentDoneMsg struct{}

// runTask launches the task in a background goroutine and returns a tea.Cmd
// that blocks on the event channel until the first event arrives.
func (m *Model) runTask(input string) tea.Cmd {
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
