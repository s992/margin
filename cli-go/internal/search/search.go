package search

import (
	"bufio"
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

func Run(root, query string, groups []string, limit int) ([]Result, error) {
	if strings.TrimSpace(query) == "" {
		return []Result{}, nil
	}
	paths := rootio.ResolvePathGroups(root, groups)
	if len(paths) == 0 {
		return []Result{}, nil
	}
	if _, err := exec.LookPath("rg"); err == nil {
		res, err := runRipgrep(root, query, paths, limit)
		if err == nil {
			return res, nil
		}
	}
	return runFallback(root, query, paths, limit)
}

func runRipgrep(root, query string, paths []string, limit int) ([]Result, error) {
	args := []string{"--json", "--line-number", "--column", "--no-heading", "--smart-case", query}
	args = append(args, paths...)
	cmd := exec.Command("rg", args...)
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

	results := make([]Result, 0, 64)
	s := bufio.NewScanner(stdout)
	for s.Scan() {
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
	_ = s.Err()
	stderrBytes, _ := io.ReadAll(stderr)
	err = cmd.Wait()
	if err != nil {
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

func runFallback(root, query string, paths []string, limit int) ([]Result, error) {
	files, err := rootio.ListFilesRecursive(paths)
	if err != nil {
		return nil, err
	}
	results := make([]Result, 0, 64)
	qLower := strings.ToLower(query)
	for _, f := range files {
		file, err := os.Open(f)
		if err != nil {
			continue
		}
		s := bufio.NewScanner(file)
		ln := 0
		for s.Scan() {
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
		_ = file.Close()
	}
	return results, nil
}
