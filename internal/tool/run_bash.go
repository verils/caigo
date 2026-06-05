package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

var RunBash = Func{
	Desc: Description{
		Name:        "run_bash",
		DisplayName: "RunBash",
		Description: "Execute a bash command and return its output.",
		Input:       `{"type":"object","properties":{"command":{"type":"string","description":"Bash command to execute"}},"required":["command"]}`,
	},
	Fn: func(ctx context.Context, input string) (string, error) {
		var args struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal([]byte(input), &args); err != nil {
			return "", fmt.Errorf("run_bash: parse input: %w", err)
		}

		cmd := exec.CommandContext(ctx, "bash", "-c", args.Command)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err := cmd.Run()
		output := stdout.String()
		if s := stderr.String(); s != "" {
			if output != "" {
				output += "\n"
			}
			output += "[stderr] " + s
		}
		if err != nil {
			if output != "" {
				return output + "\n[exit] " + err.Error(), nil
			}
			return "", fmt.Errorf("run_bash: %w", err)
		}
		return output, nil
	},
}
