package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

var WriteFile = Func{
	Desc: Description{
		Name:        "write_file",
		DisplayName: "WriteFile",
		Description: "Write content to a file at the given path. Creates parent directories if needed.",
		Input:       `{"type":"object","properties":{"path":{"type":"string","description":"Path to the file to write"},"content":{"type":"string","description":"Content to write to the file"}},"required":["path","content"]}`,
	},
	Fn: func(_ context.Context, input string) (string, error) {
		var args struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(input), &args); err != nil {
			return "", fmt.Errorf("write_file: parse input: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(args.Path), 0o755); err != nil {
			return "", fmt.Errorf("write_file: create dir: %w", err)
		}
		if err := os.WriteFile(args.Path, []byte(args.Content), 0o644); err != nil {
			return "", fmt.Errorf("write_file: %w", err)
		}
		return fmt.Sprintf("wrote %d bytes to %s", len(args.Content), args.Path), nil
	},
}
