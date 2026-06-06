package session

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/verils/caigo/internal/llm"
	"github.com/verils/caigo/internal/message"
	"github.com/verils/caigo/internal/tool"
)

func TestTaskRunStreamsModelAndExecutesTool(t *testing.T) {
	ctx := context.Background()
	m := &fakeModel{}
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

	task := NewTask(m, []tool.Tool{echo})
	sess := New(message.User("say hello"))

	var stream strings.Builder
	var eventTypes []EventType
	added, err := task.Run(ctx, sess, func(ev Event) error {
		eventTypes = append(eventTypes, ev.Type)
		if ev.Type == EventContentDelta {
			stream.WriteString(ev.Delta)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(added) != 3 {
		t.Fatalf("added messages = %d, want 3", len(added))
	}

	// First: assistant with tool call
	if len(added[0].ToolCalls) != 1 || added[0].ToolCalls[0].Name != "echo" {
		t.Fatalf("assistant tool call message = %#v", added[0])
	}
	// Second: tool result
	if added[1].Role != message.RoleTool || added[1].Content != "echo:hello" {
		t.Fatalf("tool result message = %#v", added[1])
	}
	// Third: final assistant
	if added[2].Role != message.RoleAssistant || added[2].Content != "done: echo:hello" {
		t.Fatalf("final assistant message = %#v", added[2])
	}

	if m.calls != 2 {
		t.Fatalf("model calls = %d, want 2", m.calls)
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

	// Verify session was updated
	messages, err := sess.Messages(ctx)
	if err != nil {
		t.Fatalf("session.Messages() error = %v", err)
	}
	if len(messages) != 4 { // user + 3 added
		t.Fatalf("session messages = %d, want 4", len(messages))
	}
}

type fakeModel struct {
	calls int
}

func (m *fakeModel) Stream(ctx context.Context, req llm.Request, emit func(llm.Event) error) error {
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
		if err := emit(llm.Event{Type: llm.EventContentDelta, Delta: "thinking..."}); err != nil {
			return err
		}
		return emit(llm.Event{
			Type: llm.EventToolCall,
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
		if err := emit(llm.Event{Type: llm.EventContentDelta, Delta: "done: " + last.Content}); err != nil {
			return err
		}
		return emit(llm.Event{Type: llm.EventFinish, FinishReason: "stop"})
	default:
		return fmt.Errorf("unexpected model call %d", m.calls)
	}
}
