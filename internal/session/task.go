package session

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/verils/caigo/internal/message"
	"github.com/verils/caigo/internal/model"
	"github.com/verils/caigo/internal/tool"
)

var ErrNoModel = errors.New("session: model is required")

type EventType string

const (
	EventContentDelta EventType = "content_delta"
	EventToolCall     EventType = "tool_call"
	EventToolResult   EventType = "tool_result"
)

type Event struct {
	Type       EventType
	Delta      string
	ToolCall   *message.ToolCall
	ToolResult *message.Message
}

type Task struct {
	Model model.Model
	Tools []tool.Tool
}

func NewTask(m model.Model, tools []tool.Tool) *Task {
	return &Task{
		Model: m,
		Tools: tools,
	}
}

// Run processes a conversation turn using the provided session.
// It appends new messages to the session and returns only the newly generated messages.
// The loop continues until the model signals completion via finishReason: stop.
func (t *Task) Run(ctx context.Context, session Session, emit func(Event) error) ([]message.Message, error) {
	if t.Model == nil {
		return nil, ErrNoModel
	}

	history, err := session.Messages(ctx)
	if err != nil {
		return nil, fmt.Errorf("session: failed to get messages: %w", err)
	}

	indexed := indexTools(t.Tools)
	local := append([]message.Message(nil), history...)
	var added []message.Message

	for {
		assistant, finished, err := t.runModelTurn(ctx, local, indexed.descriptions, emit)
		if err != nil {
			return added, err
		}

		if err := session.Append(ctx, assistant); err != nil {
			return added, fmt.Errorf("session: failed to append assistant message: %w", err)
		}
		local = append(local, assistant)
		added = append(added, assistant)

		if finished {
			return added, nil
		}

		if len(assistant.ToolCalls) == 0 {
			return added, nil
		}

		for _, call := range assistant.ToolCalls {
			result := runTool(ctx, indexed.byName, call)

			if err := session.Append(ctx, result); err != nil {
				return added, fmt.Errorf("session: failed to append tool result: %w", err)
			}
			local = append(local, result)
			added = append(added, result)

			if emit != nil {
				r := result.Clone()
				if err := emit(Event{Type: EventToolResult, ToolResult: &r}); err != nil {
					return added, err
				}
			}
		}
	}
}

func (t *Task) runModelTurn(ctx context.Context, history []message.Message, tools []tool.Description, emit func(Event) error) (message.Message, bool, error) {
	assistant := message.Message{Role: message.RoleAssistant}
	var finished bool

	req := model.Request{Messages: history, Tools: tools}
	err := t.Model.Stream(ctx, req, func(ev model.Event) error {
		switch ev.Type {
		case model.EventContentDelta:
			assistant.Content += ev.Delta
			if emit != nil {
				return emit(Event{Type: EventContentDelta, Delta: ev.Delta})
			}
		case model.EventToolCall:
			if ev.ToolCall == nil {
				return errors.New("session: nil tool call")
			}
			call := ev.ToolCall.Clone()
			if call.ID == "" {
				call.ID = fmt.Sprintf("call_%d", len(assistant.ToolCalls)+1)
			}
			assistant.ToolCalls = append(assistant.ToolCalls, call)
			if emit != nil {
				call := call
				return emit(Event{Type: EventToolCall, ToolCall: &call})
			}
		case model.EventFinish:
			if ev.FinishReason == "stop" {
				finished = true
			}
		default:
			return fmt.Errorf("session: unknown model event type %q", ev.Type)
		}
		return nil
	})
	if err != nil {
		return message.Message{}, false, err
	}

	return assistant, finished, nil
}

type indexedTools struct {
	byName       map[string]tool.Tool
	descriptions []tool.Description
}

func indexTools(tools []tool.Tool) indexedTools {
	out := indexedTools{byName: map[string]tool.Tool{}}
	for _, t := range tools {
		if t == nil {
			continue
		}
		desc := t.Description()
		if desc.Name == "" {
			continue
		}
		out.byName[desc.Name] = t
		out.descriptions = append(out.descriptions, desc)
	}
	sort.Slice(out.descriptions, func(i, j int) bool {
		return out.descriptions[i].Name < out.descriptions[j].Name
	})
	return out
}

func runTool(ctx context.Context, tools map[string]tool.Tool, call message.ToolCall) message.Message {
	t, ok := tools[call.Name]
	if !ok {
		return message.ToolResult(call.ID, call.Name, "tool not found: "+call.Name)
	}

	output, err := t.Call(ctx, call.Input)
	if err != nil {
		output = "tool error: " + err.Error()
	}
	return message.ToolResult(call.ID, call.Name, output)
}
