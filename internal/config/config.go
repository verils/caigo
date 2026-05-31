package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Provider describes an API provider (e.g. OpenAI, Xiaomi MiMo).
type Provider struct {
	Name    string `json:"name"`
	BaseURL string `json:"baseUrl"`
	APIKey  string `json:"apiKey"`
	Type    string `json:"type"` // "openai-compatible"
}

// Model describes a model configuration.
type Model struct {
	Name              string `json:"name"`
	Provider          string `json:"provider"`
	ContextWindowSize int    `json:"contextWindowSize"`
}

// Config is the top-level configuration loaded from ~/.caigo/config.json.
type Config struct {
	Providers map[string]Provider `json:"providers"`
	Models    map[string]Model    `json:"models"`
	Model     string              `json:"model"` // default model key
}

// ResolvedModel holds the information needed to construct a model client.
type ResolvedModel struct {
	BaseURL           string
	APIKey            string
	Model             string // model name to send to the API
	ContextWindowSize int
}

// Load reads the config file from ~/.caigo/config.json.
// Returns a zero-value Config if the file does not exist.
func Load() (Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, fmt.Errorf("config: get home dir: %w", err)
	}
	path := filepath.Join(home, ".caigo", "config.json")

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("config: read %s: %w", path, err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("config: parse %s: %w", path, err)
	}
	return cfg, nil
}

// Resolve looks up a model by key and resolves its provider details.
// If modelKey is empty, the default model from the config is used.
func (c Config) Resolve(modelKey string) (ResolvedModel, error) {
	if modelKey == "" {
		modelKey = c.Model
	}
	if modelKey == "" {
		return ResolvedModel{}, fmt.Errorf("config: no model specified")
	}

	m, ok := c.Models[modelKey]
	if !ok {
		return ResolvedModel{}, fmt.Errorf("config: model %q not found", modelKey)
	}

	p, ok := c.Providers[m.Provider]
	if !ok {
		return ResolvedModel{}, fmt.Errorf("config: provider %q not found", m.Provider)
	}

	return ResolvedModel{
		BaseURL:           p.BaseURL,
		APIKey:            p.APIKey,
		Model:             m.Name,
		ContextWindowSize: m.ContextWindowSize,
	}, nil
}

// HasModel reports whether the config contains the given model key.
// If modelKey is empty, it checks for any default model.
func (c Config) HasModel(modelKey string) bool {
	if modelKey == "" {
		return c.Model != "" && len(c.Models) > 0
	}
	_, ok := c.Models[modelKey]
	return ok
}

// Save writes the config to ~/.caigo/config.json.
func (c Config) Save() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("config: get home dir: %w", err)
	}
	dir := filepath.Join(home, ".caigo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("config: create dir: %w", err)
	}
	path := filepath.Join(dir, "config.json")

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("config: write %s: %w", path, err)
	}
	return nil
}
