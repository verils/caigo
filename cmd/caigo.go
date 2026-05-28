package cmd

import (
	"fmt"
	"os"

	"github.com/verils/caigo/caigo/agent"
	"github.com/verils/caigo/caigo/model/openai"
	"github.com/verils/caigo/caigo/session"
	"github.com/verils/caigo/caigo/tui"
)

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "OPENAI_API_KEY is required")
		os.Exit(1)
	}

	baseURL := os.Getenv("OPENAI_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	modelName := os.Getenv("OPENAI_MODEL")
	if modelName == "" {
		modelName = "gpt-4o"
	}

	m := openai.New(
		openai.WithAPIKey(apiKey),
		openai.WithBaseURL(baseURL),
		openai.WithModel(modelName),
		openai.WithContextWindowSize(128000),
	)

	sess := session.New()
	ag := agent.New(m)
	ag.Session = sess

	if err := tui.Run(tui.Config{
		Agent:             ag,
		ModelName:         modelName,
		ContextWindowSize: 128000,
		ContextEstimator:  sess,
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
