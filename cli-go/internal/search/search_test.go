package search

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunFallbackHandlesLongLines(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "inbox")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	longPrefix := strings.Repeat("a", 120000)
	content := longPrefix + " needle " + strings.Repeat("b", 100)
	if err := os.WriteFile(filepath.Join(dir, "note.md"), []byte(content+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := runFallback(context.Background(), root, "needle", []string{dir}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 {
		t.Fatalf("expected 1 result, got %d", len(res))
	}
	if res[0].Col <= 0 {
		t.Fatalf("expected positive column, got %d", res[0].Col)
	}
}
