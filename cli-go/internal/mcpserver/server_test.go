package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSafeAppendPathRestrictsTargets(t *testing.T) {
	srv := NewWithIO(t.TempDir(), false, nil, nil, nil)
	if _, err := srv.safeAppendPath("inbox/test.md"); err != nil {
		t.Fatalf("expected inbox path to be allowed: %v", err)
	}
	if _, err := srv.safeAppendPath("notes/test.md"); err == nil {
		t.Fatal("expected non-whitelisted path to fail")
	}
}

func TestSearchToolRequiresQuery(t *testing.T) {
	srv := NewWithIO(t.TempDir(), true, []string{"inbox"}, nil, nil)
	_, err := srv.searchTool(context.Background(), searchArgs{})
	if err == nil {
		t.Fatal("expected query error")
	}
}

func TestClampedLimit(t *testing.T) {
	if got := clampedLimit(-1.0, 20); got != 20 {
		t.Fatalf("got %d", got)
	}
	if got := clampedLimit(9999.0, 20); got != maxToolLimit {
		t.Fatalf("got %d", got)
	}
	if got := clampedLimit(30.0, 20); got != 30 {
		t.Fatalf("got %d", got)
	}
}

func TestReadFileToolWithLineRange(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "inbox", "note.md")
	if err := os.WriteFile(path, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := NewWithIO(root, true, nil, nil, nil)
	out, err := srv.readFileTool(context.Background(), readFileArgs{Path: "inbox/note.md", StartLine: 2, EndLine: 3})
	if err != nil {
		t.Fatal(err)
	}
	if out.Content != "b\nc" {
		t.Fatalf("unexpected content: %q", out.Content)
	}
}

func TestAppendWriteErrorsPropagate(t *testing.T) {
	root := t.TempDir()
	blockingFile := filepath.Join(root, "inbox")
	if err := os.WriteFile(blockingFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := NewWithIO(root, false, []string{"inbox"}, nil, nil)
	_, err := srv.appendTool(context.Background(), appendArgs{Path: "inbox/test.md", Content: "hello"})
	if err == nil {
		t.Fatal("expected write error")
	}
}
