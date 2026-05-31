package main

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/verils/caigo/internal/agent"
	"github.com/verils/caigo/internal/config"
	"github.com/verils/caigo/internal/model/openai"
	"github.com/verils/caigo/internal/session"
	"github.com/verils/caigo/internal/tool"
	"github.com/verils/caigo/internal/tui"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	modelKey := os.Getenv("CAIGO_MODEL")

	if !cfg.HasModel(modelKey) {
		cfg = promptSetup()
	}

	resolved, err := cfg.Resolve(modelKey)
	if err != nil {
		slog.Error("failed to resolve model", "error", err)
		os.Exit(1)
	}

	contextWindowSize := 128000
	if resolved.ContextWindowSize > 0 {
		contextWindowSize = resolved.ContextWindowSize
	}

	m := openai.New(
		openai.WithAPIKey(resolved.APIKey),
		openai.WithBaseURL(resolved.BaseURL),
		openai.WithModel(resolved.Model),
		openai.WithContextWindowSize(contextWindowSize),
	)

	sess := session.New()
	ag := agent.New(m, tool.ReadFile, tool.WriteFile, tool.RunPwsh, tool.RunBash)

	if err := tui.Run(tui.Config{
		Agent:             ag,
		Session:           sess,
		ModelName:         resolved.Model,
		ContextWindowSize: contextWindowSize,
		ContextEstimator:  sess,
	}); err != nil {
		slog.Error("tui exited with error", "error", err)
		os.Exit(1)
	}
}

func promptSetup() config.Config {
	stdin := bufio.NewReader(os.Stdin)

	fmt.Println("No configuration found. Let's set up Caigo.")
	fmt.Println()

	baseURL := prompt(stdin, "Base URL (e.g. https://api.openai.com/v1)", "https://api.openai.com/v1")
	apiKey := prompt(stdin, "API Key", "")
	if apiKey == "" {
		slog.Error("API Key is required")
		os.Exit(1)
	}
	modelName := prompt(stdin, "Model name (e.g. gpt-4o)", "gpt-4o")

	cfg := config.Config{
		Model: modelName,
		Providers: map[string]config.Provider{
			"default": {
				Name:    "Default",
				BaseURL: baseURL,
				APIKey:  apiKey,
				Type:    "openai-compatible",
			},
		},
		Models: map[string]config.Model{
			modelName: {
				Name:              modelName,
				Provider:          "default",
				ContextWindowSize: 128000,
			},
		},
	}

	if err := cfg.Save(); err != nil {
		slog.Warn("failed to save config", "error", err)
	} else {
		fmt.Println("Configuration saved to ~/.caigo/config.json")
	}
	fmt.Println()

	return cfg
}

func prompt(r *bufio.Reader, label, defaultVal string) string {
	if defaultVal != "" {
		fmt.Printf("  %s [%s]: ", label, defaultVal)
	} else {
		fmt.Printf("  %s: ", label)
	}
	line, _ := r.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultVal
	}
	return line
}
