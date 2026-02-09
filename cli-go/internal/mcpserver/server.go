package mcpserver

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"margin/internal/rootio"
	"margin/internal/search"
)

type Server struct {
	Root     string
	Readonly bool
	Paths    []string
	in       *bufio.Reader
	out      io.Writer
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type RecentItem struct {
	Path    string `json:"path"`
	Mtime   string `json:"mtime"`
	Preview string `json:"preview"`
}

func New(root string, readonly bool, paths []string) *Server {
	return &Server{
		Root:     root,
		Readonly: readonly,
		Paths:    paths,
		in:       bufio.NewReader(os.Stdin),
		out:      os.Stdout,
	}
}

func (s *Server) Run() error {
	for {
		msg, err := s.readMessage()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		var req rpcRequest
		if err := json.Unmarshal(msg, &req); err != nil {
			continue
		}
		if req.Method == "" {
			continue
		}
		if req.ID == nil {
			_ = s.handleNotification(req)
			continue
		}
		resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
		result, rpcErr := s.handleRequest(req)
		if rpcErr != nil {
			resp.Error = rpcErr
		} else {
			resp.Result = result
		}
		if err := s.writeMessage(resp); err != nil {
			return err
		}
	}
}

func (s *Server) handleNotification(req rpcRequest) error {
	return nil
}

func (s *Server) handleRequest(req rpcRequest) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "margin",
				"version": "0.1.0",
			},
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": s.toolSpecs()}, nil
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &rpcError{Code: -32602, Message: err.Error()}
		}
		return s.callTool(p.Name, p.Arguments)
	default:
		return nil, &rpcError{Code: -32601, Message: "method not found"}
	}
}

func (s *Server) toolSpecs() []map[string]any {
	tools := []map[string]any{
		{
			"name":        "search",
			"description": "Search notes",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}, "limit": map[string]any{"type": "number"}, "paths": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}}, "required": []string{"query"}},
		},
		{
			"name":        "read_file",
			"description": "Read file under margin root",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}, "start_line": map[string]any{"type": "number"}, "end_line": map[string]any{"type": "number"}}, "required": []string{"path"}},
		},
		{
			"name":        "recent",
			"description": "List recent files",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"limit": map[string]any{"type": "number"}, "since": map[string]any{"type": "string"}}},
		},
	}
	if !s.Readonly {
		tools = append(tools, map[string]any{
			"name":        "append",
			"description": "Append text under scratch/inbox/slack",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"}}, "required": []string{"content"}},
		})
	}
	return tools
}

func (s *Server) callTool(name string, args map[string]any) (any, *rpcError) {
	switch name {
	case "search":
		query, _ := args["query"].(string)
		limit := int(numberArg(args["limit"], 20))
		paths := stringSliceArg(args["paths"])
		if len(paths) == 0 {
			paths = s.Paths
		}
		res, err := search.Run(s.Root, query, paths, limit)
		if err != nil {
			return toolError(err), nil
		}
		return toolResult(res), nil
	case "read_file":
		p, _ := args["path"].(string)
		abs, err := s.safePath(p)
		if err != nil {
			return toolError(err), nil
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return toolError(err), nil
		}
		content := string(data)
		start := int(numberArg(args["start_line"], 0))
		end := int(numberArg(args["end_line"], 0))
		if start > 0 || end > 0 {
			lines := strings.Split(content, "\n")
			if start < 1 {
				start = 1
			}
			if end <= 0 || end > len(lines) {
				end = len(lines)
			}
			if start <= end && start <= len(lines) {
				content = strings.Join(lines[start-1:end], "\n")
			} else {
				content = ""
			}
		}
		return toolResult(map[string]any{"path": filepath.ToSlash(p), "content": content}), nil
	case "recent":
		limit := int(numberArg(args["limit"], 20))
		sinceRaw, _ := args["since"].(string)
		var since time.Time
		if sinceRaw != "" {
			t, err := time.Parse(time.RFC3339, sinceRaw)
			if err == nil {
				since = t
			}
		}
		files, err := rootio.ListFilesRecursive(rootio.ResolvePathGroups(s.Root, s.Paths))
		if err != nil {
			return toolError(err), nil
		}
		items := make([]RecentItem, 0, len(files))
		for _, f := range files {
			st, err := os.Stat(f)
			if err != nil {
				continue
			}
			if !since.IsZero() && st.ModTime().Before(since) {
				continue
			}
			data, _ := os.ReadFile(f)
			preview := strings.TrimSpace(firstLine(string(data)))
			if len(preview) > 180 {
				preview = preview[:180]
			}
			rel, _ := rootio.RelUnderRoot(s.Root, f)
			items = append(items, RecentItem{Path: rel, Mtime: st.ModTime().Format(time.RFC3339), Preview: preview})
		}
		sortByMtimeDesc(items)
		if limit > 0 && len(items) > limit {
			items = items[:limit]
		}
		return toolResult(items), nil
	case "append":
		if s.Readonly {
			return toolError(errors.New("readonly mode")), nil
		}
		content, _ := args["content"].(string)
		if strings.TrimSpace(content) == "" {
			return toolError(errors.New("content is required")), nil
		}
		p, _ := args["path"].(string)
		if p == "" {
			p = filepath.ToSlash(filepath.Join("inbox", time.Now().Format("20060102T150405")+".md"))
		}
		abs, err := s.safeAppendPath(p)
		if err != nil {
			return toolError(err), nil
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return toolError(err), nil
		}
		fh, err := os.OpenFile(abs, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return toolError(err), nil
		}
		_, _ = fh.WriteString(content)
		_ = fh.Close()
		rel, _ := rootio.RelUnderRoot(s.Root, abs)
		return toolResult(map[string]any{"path": rel, "appended": len(content)}), nil
	default:
		return nil, &rpcError{Code: -32602, Message: "unknown tool"}
	}
}

func (s *Server) safePath(rel string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(rel))
	abs := filepath.Join(s.Root, clean)
	_, err := rootio.RelUnderRoot(s.Root, abs)
	if err != nil {
		return "", fmt.Errorf("path outside root")
	}
	return abs, nil
}

func (s *Server) safeAppendPath(rel string) (string, error) {
	abs, err := s.safePath(rel)
	if err != nil {
		return "", err
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel)))
	if !strings.HasPrefix(clean, "scratch/") &&
		!strings.HasPrefix(clean, "inbox/") &&
		!strings.HasPrefix(clean, "slack/") {
		return "", fmt.Errorf("append path must be under scratch/, inbox/, or slack/")
	}
	return abs, nil
}

func numberArg(v any, def float64) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case json.Number:
		f, _ := t.Float64()
		return f
	default:
		return def
	}
}

func stringSliceArg(v any) []string {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, e := range raw {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func toolResult(v any) map[string]any {
	b, _ := json.Marshal(v)
	return map[string]any{
		"content":           []map[string]any{{"type": "text", "text": string(b)}},
		"structuredContent": v,
		"isError":           false,
	}
}

func toolError(err error) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": err.Error()}},
		"isError": true,
	}
}

func sortByMtimeDesc(items []RecentItem) {
	sort.Slice(items, func(i, j int) bool {
		return items[i].Mtime > items[j].Mtime
	})
}

func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return s[:idx]
	}
	return s
}

func (s *Server) readMessage() ([]byte, error) {
	length := -1
	for {
		line, err := s.in.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(parts[0]), "Content-Length") {
			v, err := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err == nil {
				length = v
			}
		}
	}
	if length < 0 {
		return nil, fmt.Errorf("missing content-length")
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(s.in, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func (s *Server) writeMessage(v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	headers := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	if _, err := io.Copy(s.out, bytes.NewBufferString(headers)); err != nil {
		return err
	}
	_, err = s.out.Write(body)
	return err
}
