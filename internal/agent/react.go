package agent

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/verils/caigo/internal/message"
	"github.com/verils/caigo/internal/model"
	"github.com/verils/caigo/internal/tool"
)

const defaultMaxTurns = 8

var (
	ErrNoModel  = errors.New("agent: model is required")
	ErrMaxTurns = errors.New("agent: max turns reached")
)

type Agent struct {
	Model    model.Model
	Tools    []tool.Tool
	MaxTurns int
}

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

func New(m model.Model, tools ...tool.Tool) *Agent {
	return &Agent{
		Model:    m,
		Tools:    tools,
		MaxTurns: defaultMaxTurns,
	}
}

// Run processes a conversation turn given the full message history.
// It returns only the new messages generated during this turn (assistant + tool results).
// The caller is responsible for managing session state.
func (a *Agent) Run(ctx context.Context, history []message.Message, emit func(Event) error) ([]message.Message, error) {
	if a.Model == nil {
		return nil, ErrNoModel
	}

	maxTurns := a.MaxTurns
	if maxTurns <= 0 {
		maxTurns = defaultMaxTurns
	}

	tools := indexTools(a.Tools)
	local := append([]message.Message(nil), history...)
	var added []message.Message

	for turn := 0; turn < maxTurns; turn++ {
		assistant, err := a.runModelTurn(ctx, local, tools.descriptions, emit)
		if err != nil {
			return added, err
		}
		local = append(local, assistant)
		added = append(added, assistant)

		if len(assistant.ToolCalls) == 0 {
			return added, nil
		}

		for _, call := range assistant.ToolCalls {
			result := runTool(ctx, tools.byName, call)
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

	return added, fmt.Errorf("%w: %d", ErrMaxTurns, maxTurns)
}

func (a *Agent) runModelTurn(ctx context.Context, history []message.Message, tools []tool.Description, emit func(Event) error) (message.Message, error) {
	assistant := message.Message{Role: message.RoleAssistant}
	req := model.Request{Messages: history, Tools: tools}
	err := a.Model.Stream(ctx, req, func(ev model.Event) error {
		switch ev.Type {
		case model.EventContentDelta:
			assistant.Content += ev.Delta
			if emit != nil {
				return emit(Event{Type: EventContentDelta, Delta: ev.Delta})
			}
		case model.EventToolCall:
			if ev.ToolCall == nil {
				return errors.New("agent: nil tool call")
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
		default:
			return fmt.Errorf("agent: unknown model event type %q", ev.Type)
		}
		return nil
	})
	if err != nil {
		return message.Message{}, err
	}

	return assistant, nil
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
