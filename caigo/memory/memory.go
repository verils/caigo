package memory

import (
	"context"
	"sync"

	"github.com/verils/caigo/caigo/message"
)

type Memory interface {
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
