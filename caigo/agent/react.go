package agent

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/verils/caigo/caigo/message"
	"github.com/verils/caigo/caigo/model"
	"github.com/verils/caigo/caigo/session"
	"github.com/verils/caigo/caigo/tool"
)

const defaultMaxTurns = 8

var (
	ErrNoModel  = errors.New("agent: model is required")
	ErrMaxTurns = errors.New("agent: max turns reached")
)

type Agent struct {
	Model    model.Model
	Session  session.Session
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
		Session:  session.New(),
		Tools:    tools,
		MaxTurns: defaultMaxTurns,
	}
}

func (a *Agent) Run(ctx context.Context, input string, emit func(Event) error) (message.Message, error) {
	if a.Model == nil {
		return message.Message{}, ErrNoModel
	}
	if a.Session == nil {
		a.Session = session.New()
	}

	maxTurns := a.MaxTurns
	if maxTurns <= 0 {
		maxTurns = defaultMaxTurns
	}

	tools := indexTools(a.Tools)
	if err := a.Session.Append(ctx, message.User(input)); err != nil {
		return message.Message{}, err
	}

	for turn := 0; turn < maxTurns; turn++ {
		assistant, err := a.runModelTurn(ctx, tools.descriptions, emit)
		if err != nil {
			return message.Message{}, err
		}
		if err := a.Session.Append(ctx, assistant); err != nil {
			return message.Message{}, err
		}
		if len(assistant.ToolCalls) == 0 {
			return assistant, nil
		}

		for _, call := range assistant.ToolCalls {
			result := runTool(ctx, tools.byName, call)
			if err := a.Session.Append(ctx, result); err != nil {
				return message.Message{}, err
			}
			if emit != nil {
				result := result.Clone()
				if err := emit(Event{Type: EventToolResult, ToolResult: &result}); err != nil {
					return message.Message{}, err
				}
			}
		}
	}

	return message.Message{}, fmt.Errorf("%w: %d", ErrMaxTurns, maxTurns)
}

func (a *Agent) runModelTurn(ctx context.Context, tools []tool.Description, emit func(Event) error) (message.Message, error) {
	messages, err := a.Session.Messages(ctx)
	if err != nil {
		return message.Message{}, err
	}

	assistant := message.Message{Role: message.RoleAssistant}
	req := model.Request{Messages: messages, Tools: tools}
	err = a.Model.Stream(ctx, req, func(ev model.Event) error {
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
