package tui

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/verils/caigo/internal/llm"
	"github.com/verils/caigo/internal/session"
	"github.com/verils/caigo/internal/tool"
)

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

	case tea.MouseMsg:
		m.viewport, _ = m.viewport.Update(msg)
		return m, nil

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
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// --- helpers ---

func (m *Model) updateContent() tea.Cmd {
	var b strings.Builder
	b.WriteString(renderHeader())
	b.WriteString(Conversation{Width: m.width, Entries: m.conversation, StreamBuf: m.streamBuf}.Render())
	b.WriteString(InputArea{Width: m.width, InputView: m.input.View()}.Render())
	b.WriteString(StatusBar{Width: m.width, ModelName: m.modelName, CtxWindowSize: m.ctxWindowSize, CtxEstimator: m.ctxEstimator}.Render())
	b.WriteString(Hint{Width: m.width, Text: m.quitHint, Expiry: m.quitHintExpiry}.Render())
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
