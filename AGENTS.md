# AGENTS.md

This file provides guidance to the AI agent when working with code in this repository.

## Build

Uses [Taskfile](https://taskfile.dev). Run `task` (default) or `task build` to compile. Binary outputs to `build/`.

```
task build    # compile
task run      # build + run
task test     # go test -race -cover ./...
task lint     # golangci-lint
task fmt      # gofmt + goimports
task clean    # remove build/
```

## Entry point

Single file at `cmd/caigo.go` (not `cmd/caigo/main.go`). Build target is `./cmd`.

## Dependencies

Uses **Bubble Tea v2** via `charm.land` import paths (not `github.com/charmbracelet`):

```go
import (
    "charm.land/bubbles/v2/textinput"
    tea "charm.land/bubbletea/v2"
    "charm.land/lipgloss/v2"
)
```

## Architecture notes

- `llm.Model` — streaming LLM interface; only implementation is `internal/llm/openai` (OpenAI-compatible API).
- `tool.Tool` — plugin interface; built-in tools in `internal/tool/` (file read/write, bash, powershell).
- `session.Session` — message history store; `InMemory` is the only implementation.
- TUI uses `tea.Println` to push conversation entries above the frame (scrollback-friendly), not a viewport widget.

## Config

Runtime config lives at `~/.caigo/config.json`. Env var `CAIGO_MODEL` overrides the active model.
