package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestListFiles_SingleFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello"), 0o644)

	input, _ := json.Marshal(map[string]string{"path": path})
	out, err := ListFiles.Call(context.Background(), string(input))
	if err != nil {
		t.Fatal(err)
	}
	if out != "test.txt" {
		t.Fatalf("got %q", out)
	}
}

func TestListFiles_Directory(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), nil, 0o644)
	os.Mkdir(filepath.Join(dir, "sub"), 0o755)

	input, _ := json.Marshal(map[string]string{"path": dir})
	out, err := ListFiles.Call(context.Background(), string(input))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out, "\n")
	sort.Strings(lines)
	want := []string{"a.txt", "sub/"}
	if len(lines) != len(want) || lines[0] != want[0] || lines[1] != want[1] {
		t.Fatalf("got %v, want %v", lines, want)
	}
}

func TestListFiles_Recursive(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), nil, 0o644)
	sub := filepath.Join(dir, "sub")
	os.Mkdir(sub, 0o755)
	os.WriteFile(filepath.Join(sub, "b.txt"), nil, 0o644)

	input, _ := json.Marshal(map[string]interface{}{"path": dir, "recursive": true})
	out, err := ListFiles.Call(context.Background(), string(input))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out, "\n")
	sort.Strings(lines)
	want := []string{"a.txt", "sub/", "sub/b.txt"}
	if len(lines) != len(want) {
		t.Fatalf("got %v, want %v", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("line %d: got %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestListFiles_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	input, _ := json.Marshal(map[string]string{"path": dir})
	out, err := ListFiles.Call(context.Background(), string(input))
	if err != nil {
		t.Fatal(err)
	}
	if out != "(empty directory)" {
		t.Fatalf("got %q", out)
	}
}

func TestListFiles_DefaultPath(t *testing.T) {
	_, err := ListFiles.Call(context.Background(), `{"path":""}`)
	if err != nil {
		t.Fatal(err)
	}
}
