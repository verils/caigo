package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

var ReadFile = Func{
	Desc: Description{
		Name:        "read_file",
		Description: "Read the contents of a file at the given path.",
		Input:       `{"type":"object","properties":{"path":{"type":"string","description":"Path to the file to read"}},"required":["path"]}`,
	},
	Fn: func(_ context.Context, input string) (string, error) {
		var args struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal([]byte(input), &args); err != nil {
			return "", fmt.Errorf("read_file: parse input: %w", err)
		}
		data, err := os.ReadFile(args.Path)
		if err != nil {
			return "", fmt.Errorf("read_file: %w", err)
		}
		return string(data), nil
	},
}
