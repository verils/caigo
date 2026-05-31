package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/verils/caigo/internal/message"
	"github.com/verils/caigo/internal/model"
)

const (
	defaultBaseURL           = "https://api.openai.com/v1"
	defaultModel             = "gpt-5.4"
	defaultContextWindowSize = 128000
)

type ChatCompletion struct {
	BaseURL           string
	APIKey            string
	Model             string
	ContextWindowSize int

	client *http.Client
	once   sync.Once
}

func New(opts ...Option) *ChatCompletion {
	c := &ChatCompletion{
		BaseURL:           defaultBaseURL,
		Model:             defaultModel,
		ContextWindowSize: defaultContextWindowSize,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

type Option func(*ChatCompletion)

func WithBaseURL(url string) Option {
	return func(c *ChatCompletion) { c.BaseURL = url }
}

func WithAPIKey(key string) Option {
	return func(c *ChatCompletion) { c.APIKey = key }
}

func WithModel(model string) Option {
	return func(c *ChatCompletion) { c.Model = model }
}

func WithContextWindowSize(size int) Option {
	return func(c *ChatCompletion) { c.ContextWindowSize = size }
}

func (c *ChatCompletion) httpClient() *http.Client {
	c.once.Do(func() {
		if c.client != nil {
			return
		}
		c.client = http.DefaultClient
	})
	return c.client
}

func (c *ChatCompletion) Stream(ctx context.Context, req model.Request, emit func(model.Event) error) error {
	body, err := c.buildRequest(req)
	if err != nil {
		return fmt.Errorf("openai: build request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("openai: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.httpClient().Do(httpReq)
	if err != nil {
		return fmt.Errorf("openai: send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("openai: API error (status %d): %s", resp.StatusCode, string(errBody))
	}

	return c.readStream(resp.Body, emit)
}

func (c *ChatCompletion) buildRequest(req model.Request) ([]byte, error) {
	msgs := make([]chatMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		cm := chatMessage{Role: string(m.Role), Content: m.Content}
		if m.Role == message.RoleTool {
			cm.ToolCallID = m.ToolCallID
		}
		if len(m.ToolCalls) > 0 {
			cm.ToolCalls = make([]chatToolCall, len(m.ToolCalls))
			for i, tc := range m.ToolCalls {
				cm.ToolCalls[i] = chatToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: chatFunction{
						Name:      tc.Name,
						Arguments: tc.Input,
					},
				}
			}
		}
		msgs = append(msgs, cm)
	}

	payload := chatRequest{
		Model:    c.Model,
		Messages: msgs,
		Stream:   true,
	}

	if len(req.Tools) > 0 {
		payload.Tools = make([]chatToolDesc, len(req.Tools))
		for i, td := range req.Tools {
			payload.Tools[i] = chatToolDesc{
				Type: "function",
				Function: chatFunctionDesc{
					Name:        td.Name,
					Description: td.Description,
					Parameters:  parseJSONSchema(td.Input),
				},
			}
		}
	}

	return json.Marshal(payload)
}

func (c *ChatCompletion) readStream(body io.Reader, emit func(model.Event) error) error {
	toolCallBuffers := map[int]*message.ToolCall{}

	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return fmt.Errorf("openai: parse chunk: %w", err)
		}

		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]

		if choice.FinishReason != "" {
			if err := emit(model.Event{Type: model.EventFinish, FinishReason: choice.FinishReason}); err != nil {
				return err
			}
		}

		delta := choice.Delta

		if delta.Content != "" {
			if err := emit(model.Event{Type: model.EventContentDelta, Delta: delta.Content}); err != nil {
				return err
			}
		}

		for _, tc := range delta.ToolCalls {
			idx := tc.Index
			buf, ok := toolCallBuffers[idx]
			if !ok {
				buf = &message.ToolCall{
					ID:   tc.ID,
					Name: tc.Function.Name,
				}
				toolCallBuffers[idx] = buf
			}
			if tc.ID != "" {
				buf.ID = tc.ID
			}
			if tc.Function.Name != "" {
				buf.Name = tc.Function.Name
			}
			buf.Input += tc.Function.Arguments
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("openai: read stream: %w", err)
	}

	for i := 0; i < len(toolCallBuffers); i++ {
		tc := toolCallBuffers[i]
		if tc == nil {
			continue
		}
		if err := emit(model.Event{Type: model.EventToolCall, ToolCall: tc}); err != nil {
			return err
		}
	}

	return nil
}

// --- OpenAI API types ---

type chatRequest struct {
	Model    string         `json:"model"`
	Messages []chatMessage  `json:"messages"`
	Tools    []chatToolDesc `json:"tools,omitempty"`
	Stream   bool           `json:"stream"`
}

type chatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	Name       string         `json:"name,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
}

type chatToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function chatFunction `json:"function"`
}

type chatFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatToolDesc struct {
	Type     string           `json:"type"`
	Function chatFunctionDesc `json:"function"`
}

type chatFunctionDesc struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

type streamChunk struct {
	Choices []streamChoice `json:"choices"`
}

type streamChoice struct {
	Delta        streamDelta `json:"delta"`
	FinishReason string      `json:"finish_reason,omitempty"`
}

type streamDelta struct {
	Content   string           `json:"content"`
	ToolCalls []streamToolCall `json:"tool_calls,omitempty"`
}

type streamToolCall struct {
	Index    int        `json:"index"`
	ID       string     `json:"id,omitempty"`
	Function streamFunc `json:"function"`
}

type streamFunc struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments"`
}

// parseJSONSchema converts a JSON schema string to a typed value for the API.
func parseJSONSchema(input string) any {
	if input == "" {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	var schema any
	if err := json.Unmarshal([]byte(input), &schema); err != nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return schema
}
