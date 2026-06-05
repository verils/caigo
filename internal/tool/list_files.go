package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ListFiles = Func{
	Desc: Description{
		Name:        "list_files",
		DisplayName: "ListFiles",
		Description: "List files and directories at the given path. Returns a formatted list with type indicators.",
		Input:       `{"type":"object","properties":{"path":{"type":"string","description":"Path to the directory to list"},"recursive":{"type":"boolean","description":"Whether to list files recursively (default: false)"}},"required":["path"]}`,
	},
	Fn: func(_ context.Context, input string) (string, error) {
		var args struct {
			Path      string `json:"path"`
			Recursive bool   `json:"recursive"`
		}
		if err := json.Unmarshal([]byte(input), &args); err != nil {
			return "", fmt.Errorf("list_files: parse input: %w", err)
		}

		if args.Path == "" {
			args.Path = "."
		}

		info, err := os.Stat(args.Path)
		if err != nil {
			return "", fmt.Errorf("list_files: %w", err)
		}
		if !info.IsDir() {
			return info.Name(), nil
		}

		var entries []string
		if args.Recursive {
			err = filepath.Walk(args.Path, func(path string, fi os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				rel, _ := filepath.Rel(args.Path, path)
				if rel == "." {
					return nil
				}
				// Normalize to forward slashes for consistent output
				rel = filepath.ToSlash(rel)
				if fi.IsDir() {
					entries = append(entries, rel+"/")
				} else {
					entries = append(entries, rel)
				}
				return nil
			})
		} else {
			dirEntries, readErr := os.ReadDir(args.Path)
			if readErr != nil {
				return "", fmt.Errorf("list_files: %w", readErr)
			}
			for _, e := range dirEntries {
				if e.IsDir() {
					entries = append(entries, e.Name()+"/")
				} else {
					entries = append(entries, e.Name())
				}
			}
		}
		if err != nil {
			return "", fmt.Errorf("list_files: %w", err)
		}

		if len(entries) == 0 {
			return "(empty directory)", nil
		}
		return strings.Join(entries, "\n"), nil
	},
}
