package model

import (
	"context"

	"github.com/verils/caigo/internal/message"
	"github.com/verils/caigo/internal/tool"
)

type Model interface {
	Stream(ctx context.Context, req Request, emit func(Event) error) error
}

type Request struct {
	Messages []message.Message
	Tools    []tool.Description
}

type EventType string

const (
	EventContentDelta EventType = "content_delta"
	EventToolCall     EventType = "tool_call"
	EventFinish       EventType = "finish"
)

type Event struct {
	Type         EventType
	Delta        string
	ToolCall     *message.ToolCall
	FinishReason string // set when Type == EventFinish
}
