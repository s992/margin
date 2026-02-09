package search

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"margin/internal/rootio"
)

const (
	ripgrepTimeout    = 20 * time.Second
	maxScannerToken   = 1024 * 1024
	defaultResultSize = 64
)

type Result struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	Preview string `json:"preview"`
	Mtime   string `json:"mtime"`
}

type rgEvent struct {
	Type string `json:"type"`
	Data struct {
		Path struct {
			Text string `json:"text"`
		} `json:"path"`
		LineNumber int `json:"line_number"`
		Submatches []struct {
			Start int `json:"start"`
		} `json:"submatches"`
		Lines struct {
			Text string `json:"text"`
		} `json:"lines"`
	} `json:"data"`
}

func Run(ctx context.Context, root, query string, groups []string, limit int) ([]Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(query) == "" {
		return []Result{}, nil
	}
	paths := rootio.ResolvePathGroups(root, groups)
	if len(paths) == 0 {
		return []Result{}, nil
	}
	if _, err := exec.LookPath("rg"); err == nil {
		res, err := runRipgrep(ctx, root, query, paths, limit)
		if err == nil {
			return res, nil
		}
	}
	return runFallback(ctx, root, query, paths, limit)
}

func runRipgrep(ctx context.Context, root, query string, paths []string, limit int) ([]Result, error) {
	ctx, cancel := context.WithTimeout(ctx, ripgrepTimeout)
	defer cancel()

	args := []string{"--json", "--line-number", "--column", "--no-heading", "--smart-case", query}
	args = append(args, paths...)
	cmd := exec.CommandContext(ctx, "rg", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	results := make([]Result, 0, defaultResultSize)
	s := bufio.NewScanner(stdout)
	s.Buffer(make([]byte, 64*1024), maxScannerToken)
	for s.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line := s.Bytes()
		var ev rgEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		if ev.Type != "match" {
			continue
		}
		col := 1
		if len(ev.Data.Submatches) > 0 {
			col = ev.Data.Submatches[0].Start + 1
		}
		mtime := ""
		if st, err := os.Stat(ev.Data.Path.Text); err == nil {
			mtime = st.ModTime().Format(time.RFC3339)
		}
		rel, err := rootio.RelUnderRoot(root, ev.Data.Path.Text)
		if err != nil {
			rel = filepath.ToSlash(ev.Data.Path.Text)
		}
		results = append(results, Result{
			File:    rel,
			Line:    ev.Data.LineNumber,
			Col:     col,
			Preview: strings.TrimSpace(ev.Data.Lines.Text),
			Mtime:   mtime,
		})
		if limit > 0 && len(results) >= limit {
			break
		}
	}
	if err := s.Err(); err != nil {
		return nil, fmt.Errorf("scan rg output: %w", err)
	}
	stderrBytes, _ := io.ReadAll(stderr)
	err = cmd.Wait()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("rg timeout after %s", ripgrepTimeout)
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil, ctx.Err()
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if exitErr.ExitCode() == 1 {
				return results, nil
			}
		}
		return nil, fmt.Errorf("rg failed: %s", strings.TrimSpace(string(stderrBytes)))
	}
	return results, nil
}

func runFallback(ctx context.Context, root, query string, paths []string, limit int) ([]Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	files, err := rootio.ListFilesRecursive(paths)
	if err != nil {
		return nil, err
	}
	results := make([]Result, 0, defaultResultSize)
	qLower := strings.ToLower(query)
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		file, err := os.Open(f)
		if err != nil {
			continue
		}
		s := bufio.NewScanner(file)
		s.Buffer(make([]byte, 64*1024), maxScannerToken)
		ln := 0
		for s.Scan() {
			if err := ctx.Err(); err != nil {
				_ = file.Close()
				return nil, err
			}
			ln++
			text := s.Text()
			idx := strings.Index(strings.ToLower(text), qLower)
			if idx < 0 {
				continue
			}
			rel, err := rootio.RelUnderRoot(root, f)
			if err != nil {
				rel = filepath.ToSlash(f)
			}
			mtime := ""
			if st, err := os.Stat(f); err == nil {
				mtime = st.ModTime().Format(time.RFC3339)
			}
			results = append(results, Result{
				File:    rel,
				Line:    ln,
				Col:     idx + 1,
				Preview: strings.TrimSpace(text),
				Mtime:   mtime,
			})
			if limit > 0 && len(results) >= limit {
				_ = file.Close()
				return results, nil
			}
		}
		if err := s.Err(); err != nil {
			_ = file.Close()
			continue
		}
		_ = file.Close()
	}
	return results, nil
}
