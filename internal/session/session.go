package session

import (
	"context"
	"sync"

	"github.com/verils/caigo/caigo/message"
)

type Session interface {
	Append(ctx context.Context, msg message.Message) error
	Messages(ctx context.Context) ([]message.Message, error)
}

type InMemory struct {
	mu       sync.Mutex
	messages []message.Message
}

func New(messages ...message.Message) *InMemory {
	m := &InMemory{}
	for _, msg := range messages {
		m.messages = append(m.messages, msg.Clone())
	}
	return m
}

func (m *InMemory) Append(ctx context.Context, msg message.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, msg.Clone())
	return nil
}

func (m *InMemory) Messages(ctx context.Context) ([]message.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]message.Message, len(m.messages))
	for i, msg := range m.messages {
		out[i] = msg.Clone()
	}
	return out, nil
}

// ContextTokens returns an estimated token count for all stored messages.
// Uses a rough heuristic of ~4 characters per token.
func (m *InMemory) ContextTokens() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	chars := 0
	for _, msg := range m.messages {
		chars += len(msg.Content) + len(msg.Name) + len(msg.ToolCallID)
		for _, tc := range msg.ToolCalls {
			chars += len(tc.Name) + len(tc.Input)
		}
	}
	// Rough estimate: ~4 chars per token, plus ~4 overhead per message
	return (chars+4*len(m.messages))/4 + 1
}
