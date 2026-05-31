package main

import (
	"fmt"
	"os"

	"github.com/verils/caigo/internal/agent"
	"github.com/verils/caigo/internal/config"
	"github.com/verils/caigo/internal/model/openai"
	"github.com/verils/caigo/internal/session"
	"github.com/verils/caigo/internal/tui"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	modelKey := os.Getenv("CAIGO_MODEL")

	var (
		apiKey            string
		baseURL           string
		modelName         string
		contextWindowSize = 128000
	)

	if cfg.HasModel(modelKey) {
		resolved, err := cfg.Resolve(modelKey)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		apiKey = resolved.APIKey
		baseURL = resolved.BaseURL
		modelName = resolved.Model
		if resolved.ContextWindowSize > 0 {
			contextWindowSize = resolved.ContextWindowSize
		}
	} else {
		// Fallback to environment variables.
		apiKey = os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			fmt.Fprintln(os.Stderr, "OPENAI_API_KEY is required (or configure ~/.caigo/config.json)")
			os.Exit(1)
		}
		baseURL = os.Getenv("OPENAI_BASE_URL")
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
		modelName = os.Getenv("OPENAI_MODEL")
		if modelName == "" {
			modelName = "gpt-4o"
		}
	}

	m := openai.New(
		openai.WithAPIKey(apiKey),
		openai.WithBaseURL(baseURL),
		openai.WithModel(modelName),
		openai.WithContextWindowSize(contextWindowSize),
	)

	sess := session.New()
	ag := agent.New(m)
	ag.Session = sess

	if err := tui.Run(tui.Config{
		Agent:             ag,
		ModelName:         modelName,
		ContextWindowSize: contextWindowSize,
		ContextEstimator:  sess,
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
