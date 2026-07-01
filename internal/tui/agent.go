package tui

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/verils/caigo/internal/message"
	"github.com/verils/caigo/internal/session"
)

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
