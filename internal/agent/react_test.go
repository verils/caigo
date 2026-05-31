package agent

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/verils/caigo/caigo/message"
	"github.com/verils/caigo/caigo/model"
	"github.com/verils/caigo/caigo/tool"
)

func TestAgentRunStreamsModelAndExecutesTool(t *testing.T) {
	ctx := context.Background()
	model := &fakeModel{}
	var toolInputs []string
	echo := tool.Func{
		Desc: tool.Description{
			Name:        "echo",
			Description: "returns its input",
			Input:       "plain text",
		},
		Fn: func(ctx context.Context, input string) (string, error) {
			toolInputs = append(toolInputs, input)
			return "echo:" + input, nil
		},
	}

	agent := New(model, echo)
	var stream strings.Builder
	var eventTypes []EventType
	got, err := agent.Run(ctx, "say hello", func(ev Event) error {
		eventTypes = append(eventTypes, ev.Type)
		if ev.Type == EventContentDelta {
			stream.WriteString(ev.Delta)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if got.Content != "done: echo:hello" {
		t.Fatalf("final content = %q, want %q", got.Content, "done: echo:hello")
	}
	if model.calls != 2 {
		t.Fatalf("model calls = %d, want 2", model.calls)
	}
	if !reflect.DeepEqual(toolInputs, []string{"hello"}) {
		t.Fatalf("tool inputs = %#v, want %#v", toolInputs, []string{"hello"})
	}
	if stream.String() != "thinking...done: echo:hello" {
		t.Fatalf("stream = %q", stream.String())
	}

	wantEvents := []EventType{EventContentDelta, EventToolCall, EventToolResult, EventContentDelta}
	if !reflect.DeepEqual(eventTypes, wantEvents) {
		t.Fatalf("event types = %#v, want %#v", eventTypes, wantEvents)
	}

	history, err := agent.Session.Messages(ctx)
	if err != nil {
		t.Fatalf("Session.Messages() error = %v", err)
	}
	if len(history) != 4 {
		t.Fatalf("history length = %d, want 4: %#v", len(history), history)
	}
	if history[0].Role != message.RoleUser || history[0].Content != "say hello" {
		t.Fatalf("first message = %#v", history[0])
	}
	if len(history[1].ToolCalls) != 1 || history[1].ToolCalls[0].Name != "echo" {
		t.Fatalf("assistant tool call message = %#v", history[1])
	}
	if history[2].Role != message.RoleTool || history[2].Content != "echo:hello" {
		t.Fatalf("tool result message = %#v", history[2])
	}
	if history[3].Role != message.RoleAssistant || history[3].Content != got.Content {
		t.Fatalf("final assistant message = %#v", history[3])
	}
}

type fakeModel struct {
	calls int
}

func (m *fakeModel) Stream(ctx context.Context, req model.Request, emit func(model.Event) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	m.calls++
	switch m.calls {
	case 1:
		if len(req.Messages) != 1 {
			return fmt.Errorf("first request messages = %d, want 1", len(req.Messages))
		}
		if req.Messages[0].Role != message.RoleUser || req.Messages[0].Content != "say hello" {
			return fmt.Errorf("first request user message = %#v", req.Messages[0])
		}
		if len(req.Tools) != 1 || req.Tools[0].Name != "echo" {
			return fmt.Errorf("first request tools = %#v", req.Tools)
		}
		if err := emit(model.Event{Type: model.EventContentDelta, Delta: "thinking..."}); err != nil {
			return err
		}
		return emit(model.Event{
			Type: model.EventToolCall,
			ToolCall: &message.ToolCall{
				ID:    "call_echo",
				Name:  "echo",
				Input: "hello",
			},
		})
	case 2:
		if len(req.Messages) != 3 {
			return fmt.Errorf("second request messages = %d, want 3", len(req.Messages))
		}
		last := req.Messages[len(req.Messages)-1]
		if last.Role != message.RoleTool || last.ToolCallID != "call_echo" || last.Content != "echo:hello" {
			return fmt.Errorf("second request last message = %#v", last)
		}
		return emit(model.Event{Type: model.EventContentDelta, Delta: "done: " + last.Content})
	default:
		return fmt.Errorf("unexpected model call %d", m.calls)
	}
}
