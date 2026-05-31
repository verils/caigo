package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadNotExist(t *testing.T) {
	// Override home dir to a temp dir so the file won't exist.
	dir := t.TempDir()
	t.Setenv("USERPROFILE", dir) // Windows
	t.Setenv("HOME", dir)        // Unix

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Models) != 0 || len(cfg.Providers) != 0 {
		t.Fatalf("expected empty config, got %+v", cfg)
	}
}

func TestLoadAndResolve(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("USERPROFILE", dir)
	t.Setenv("HOME", dir)

	caigoDir := filepath.Join(dir, ".caigo")
	if err := os.MkdirAll(caigoDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cfgData := Config{
		Model: "mimo-v2.5-pro",
		Providers: map[string]Provider{
			"xiaomi-mimo": {
				Name:    "Xiaomi MiMo",
				BaseURL: "https://api.example.com/v1",
				APIKey:  "sk-test",
				Type:    "openai-compatible",
			},
		},
		Models: map[string]Model{
			"mimo-v2.5-pro": {
				Name:              "MiMo V2.5 Pro",
				Provider:          "xiaomi-mimo",
				ContextWindowSize: 128000,
			},
		},
	}
	data, _ := json.Marshal(cfgData)
	if err := os.WriteFile(filepath.Join(caigoDir, "config.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Resolve default model.
	resolved, err := cfg.Resolve("")
	if err != nil {
		t.Fatalf("Resolve(\"\") error = %v", err)
	}
	if resolved.BaseURL != "https://api.example.com/v1" {
		t.Errorf("BaseURL = %q, want %q", resolved.BaseURL, "https://api.example.com/v1")
	}
	if resolved.APIKey != "sk-test" {
		t.Errorf("APIKey = %q, want %q", resolved.APIKey, "sk-test")
	}
	if resolved.Model != "MiMo V2.5 Pro" {
		t.Errorf("Model = %q, want %q", resolved.Model, "MiMo V2.5 Pro")
	}
	if resolved.ContextWindowSize != 128000 {
		t.Errorf("ContextWindowSize = %d, want %d", resolved.ContextWindowSize, 128000)
	}

	// Resolve explicit model key.
	resolved2, err := cfg.Resolve("mimo-v2.5-pro")
	if err != nil {
		t.Fatalf("Resolve(\"mimo-v2.5-pro\") error = %v", err)
	}
	if resolved2.BaseURL != resolved.BaseURL {
		t.Errorf("explicit resolve mismatch")
	}

	// Resolve unknown model.
	_, err = cfg.Resolve("nonexistent")
	if err == nil {
		t.Error("Resolve(\"nonexistent\") should return error")
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("USERPROFILE", dir)
	t.Setenv("HOME", dir)

	cfg := Config{
		Model: "gpt-4o",
		Providers: map[string]Provider{
			"default": {
				Name:    "OpenAI",
				BaseURL: "https://api.openai.com/v1",
				APIKey:  "sk-abc",
				Type:    "openai-compatible",
			},
		},
		Models: map[string]Model{
			"gpt-4o": {
				Name:              "GPT-4o",
				Provider:          "default",
				ContextWindowSize: 128000,
			},
		},
	}

	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Model != "gpt-4o" {
		t.Errorf("Model = %q, want %q", loaded.Model, "gpt-4o")
	}
	if loaded.Providers["default"].APIKey != "sk-abc" {
		t.Errorf("APIKey = %q, want %q", loaded.Providers["default"].APIKey, "sk-abc")
	}
	if loaded.Models["gpt-4o"].ContextWindowSize != 128000 {
		t.Errorf("ContextWindowSize = %d, want %d", loaded.Models["gpt-4o"].ContextWindowSize, 128000)
	}
}

func TestHasModel(t *testing.T) {
	cfg := Config{
		Model: "default-model",
		Models: map[string]Model{
			"default-model": {Name: "Default", Provider: "p", ContextWindowSize: 64000},
		},
	}

	if !cfg.HasModel("") {
		t.Error("HasModel(\"\") should be true when default is set")
	}
	if !cfg.HasModel("default-model") {
		t.Error("HasModel(\"default-model\") should be true")
	}
	if cfg.HasModel("other") {
		t.Error("HasModel(\"other\") should be false")
	}
}
