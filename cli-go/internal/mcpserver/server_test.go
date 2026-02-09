package mcpserver

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadMessageRejectsOversizedPayload(t *testing.T) {
	raw := "Content-Length: 9999999\r\n\r\n"
	srv := NewWithIO(t.TempDir(), true, nil, strings.NewReader(raw), &strings.Builder{})
	_, err := srv.readMessage()
	if err == nil || !strings.Contains(err.Error(), "exceeds max") {
		t.Fatalf("expected size error, got: %v", err)
	}
}

func TestSafeAppendPathRestrictsTargets(t *testing.T) {
	srv := NewWithIO(t.TempDir(), false, nil, strings.NewReader(""), &strings.Builder{})
	if _, err := srv.safeAppendPath("inbox/test.md"); err != nil {
		t.Fatalf("expected inbox path to be allowed: %v", err)
	}
	if _, err := srv.safeAppendPath("notes/test.md"); err == nil {
		t.Fatal("expected non-whitelisted path to fail")
	}
}

func TestCallToolSearchRequiresQuery(t *testing.T) {
	srv := NewWithIO(t.TempDir(), true, []string{"inbox"}, strings.NewReader(""), &strings.Builder{})
	res, rpcErr := srv.callTool(context.Background(), "search", map[string]any{})
	if rpcErr != nil {
		t.Fatalf("unexpected rpc error: %v", rpcErr)
	}
	data, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("unexpected response type: %T", res)
	}
	if isErr, _ := data["isError"].(bool); !isErr {
		t.Fatalf("expected isError response: %#v", data)
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

func TestReadMessageRoundTrip(t *testing.T) {
	body := `{"jsonrpc":"2.0","method":"ping"}`
	raw := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
	srv := &Server{in: bufio.NewReader(strings.NewReader(raw))}
	msg, err := srv.readMessage()
	if err != nil {
		t.Fatal(err)
	}
	if string(msg) != body {
		t.Fatalf("unexpected body: %s", string(msg))
	}
}

func TestAppendWriteErrorsPropagate(t *testing.T) {
	root := t.TempDir()
	blockingFile := filepath.Join(root, "inbox")
	if err := os.WriteFile(blockingFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := NewWithIO(root, false, []string{"inbox"}, strings.NewReader(""), &strings.Builder{})
	res, rpcErr := srv.callTool(context.Background(), "append", map[string]any{"path": "inbox/test.md", "content": "hello"})
	if rpcErr != nil {
		t.Fatalf("unexpected rpc error: %v", rpcErr)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("unexpected response type: %T", res)
	}
	isErr, _ := m["isError"].(bool)
	if !isErr {
		t.Fatalf("expected write error result, got %#v", m)
	}
}
