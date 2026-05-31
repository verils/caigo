package message

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type ToolCall struct {
	ID    string
	Name  string
	Input string
}

func (c ToolCall) Clone() ToolCall {
	return c
}

type Message struct {
	Role       Role
	Content    string
	Name       string
	ToolCallID string
	ToolCalls  []ToolCall
}

func User(content string) Message {
	return Message{Role: RoleUser, Content: content}
}

func Assistant(content string, calls ...ToolCall) Message {
	return Message{Role: RoleAssistant, Content: content, ToolCalls: cloneToolCalls(calls)}
}

func ToolResult(id, name, content string) Message {
	return Message{Role: RoleTool, ToolCallID: id, Name: name, Content: content}
}

func (m Message) Clone() Message {
	m.ToolCalls = cloneToolCalls(m.ToolCalls)
	return m
}

func cloneToolCalls(calls []ToolCall) []ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]ToolCall, len(calls))
	copy(out, calls)
	return out
}
