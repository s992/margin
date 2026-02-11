package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"margin/internal/rootio"
	"margin/internal/search"
)

const (
	serverVersion      = "0.1.0"
	defaultSearchLimit = 20
	defaultRecentLimit = 20
	maxToolLimit       = 500
)

type Server struct {
	Root     string
	Readonly bool
	Paths    []string
	in       io.Reader
	out      io.Writer
}

type RecentItem struct {
	Path    string `json:"path"`
	Mtime   string `json:"mtime"`
	Preview string `json:"preview"`
}

type searchArgs struct {
	Query string   `json:"query"`
	Limit int      `json:"limit,omitempty"`
	Paths []string `json:"paths,omitempty"`
}

type readFileArgs struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
}

type recentArgs struct {
	Limit int    `json:"limit,omitempty"`
	Since string `json:"since,omitempty"`
}

type appendArgs struct {
	Path    string `json:"path,omitempty"`
	Content string `json:"content"`
}

type readFileOutput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type appendOutput struct {
	Path     string `json:"path"`
	Appended int    `json:"appended"`
}

func New(root string, readonly bool, paths []string) *Server {
	return NewWithIO(root, readonly, paths, os.Stdin, os.Stdout)
}

func NewWithIO(root string, readonly bool, paths []string, in io.Reader, out io.Writer) *Server {
	return &Server{
		Root:     root,
		Readonly: readonly,
		Paths:    paths,
		in:       in,
		out:      out,
	}
}

func (s *Server) Run(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "margin", Version: serverVersion}, nil)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "search",
		Description: "Search notes",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input searchArgs) (*mcp.CallToolResult, []search.Result, error) {
		res, err := s.searchTool(ctx, input)
		if err != nil {
			return nil, nil, err
		}
		return nil, res, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "read_file",
		Description: "Read file under margin root",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input readFileArgs) (*mcp.CallToolResult, readFileOutput, error) {
		res, err := s.readFileTool(ctx, input)
		if err != nil {
			return nil, readFileOutput{}, err
		}
		return nil, res, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "recent",
		Description: "List recent files",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input recentArgs) (*mcp.CallToolResult, []RecentItem, error) {
		res, err := s.recentTool(ctx, input)
		if err != nil {
			return nil, nil, err
		}
		return nil, res, nil
	})

	if !s.Readonly {
		mcp.AddTool(srv, &mcp.Tool{
			Name:        "append",
			Description: "Append text under scratch/inbox/slack",
		}, func(ctx context.Context, _ *mcp.CallToolRequest, input appendArgs) (*mcp.CallToolResult, appendOutput, error) {
			res, err := s.appendTool(ctx, input)
			if err != nil {
				return nil, appendOutput{}, err
			}
			return nil, res, nil
		})
	}

	in := s.in
	if in == nil {
		in = os.Stdin
	}
	out := s.out
	if out == nil {
		out = os.Stdout
	}
	transport := &mcp.IOTransport{
		Reader: io.NopCloser(in),
		Writer: nopWriteCloser{Writer: out},
	}
	return srv.Run(ctx, transport)
}

func (s *Server) searchTool(ctx context.Context, args searchArgs) ([]search.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.Query) == "" {
		return nil, errors.New("query is required")
	}
	limit := clampedLimit(float64(args.Limit), defaultSearchLimit)
	paths := args.Paths
	if len(paths) == 0 {
		paths = s.Paths
	}
	return search.Run(ctx, s.Root, args.Query, paths, limit)
}

func (s *Server) readFileTool(ctx context.Context, args readFileArgs) (readFileOutput, error) {
	if err := ctx.Err(); err != nil {
		return readFileOutput{}, err
	}
	if strings.TrimSpace(args.Path) == "" {
		return readFileOutput{}, errors.New("path is required")
	}
	abs, err := s.safePath(args.Path)
	if err != nil {
		return readFileOutput{}, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return readFileOutput{}, err
	}
	content := string(data)
	start, end := args.StartLine, args.EndLine
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
	return readFileOutput{Path: filepath.ToSlash(args.Path), Content: content}, nil
}

func (s *Server) recentTool(ctx context.Context, args recentArgs) ([]RecentItem, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	limit := clampedLimit(float64(args.Limit), defaultRecentLimit)
	var since time.Time
	if args.Since != "" {
		t, err := time.Parse(time.RFC3339, args.Since)
		if err == nil {
			since = t
		}
	}
	files, err := rootio.ListFilesRecursive(rootio.ResolvePathGroups(s.Root, s.Paths))
	if err != nil {
		return nil, err
	}
	items := make([]RecentItem, 0, len(files))
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
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
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (s *Server) appendTool(ctx context.Context, args appendArgs) (appendOutput, error) {
	if err := ctx.Err(); err != nil {
		return appendOutput{}, err
	}
	if s.Readonly {
		return appendOutput{}, errors.New("readonly mode")
	}
	if strings.TrimSpace(args.Content) == "" {
		return appendOutput{}, errors.New("content is required")
	}
	p := args.Path
	if p == "" {
		p = filepath.ToSlash(filepath.Join("inbox", time.Now().Format("20060102T150405")+".md"))
	}
	abs, err := s.safeAppendPath(p)
	if err != nil {
		return appendOutput{}, err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return appendOutput{}, err
	}
	fh, err := os.OpenFile(abs, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return appendOutput{}, err
	}
	if _, err := fh.WriteString(args.Content); err != nil {
		_ = fh.Close()
		return appendOutput{}, err
	}
	if err := fh.Close(); err != nil {
		return appendOutput{}, err
	}
	rel, _ := rootio.RelUnderRoot(s.Root, abs)
	return appendOutput{Path: rel, Appended: len(args.Content)}, nil
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

func clampedLimit(v any, def int) int {
	limit := int(numberArg(v, float64(def)))
	if limit <= 0 {
		return def
	}
	if limit > maxToolLimit {
		return maxToolLimit
	}
	return limit
}

func numberArg(v any, def float64) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case json.Number:
		f, _ := t.Float64()
		return f
	case int:
		return float64(t)
	default:
		return def
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

type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error { return nil }
