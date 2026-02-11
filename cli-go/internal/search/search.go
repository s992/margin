package search

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/blevesearch/bleve/v2"

	"margin/internal/rootio"
)

const (
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
	res, err := runBleve(ctx, root, query, paths, limit)
	if err == nil {
		return res, nil
	}
	return runFallback(ctx, root, query, paths, limit)
}

type bleveLineDoc struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Preview string `json:"preview"`
	Content string `json:"content"`
	Mtime   string `json:"mtime"`
}

func runBleve(ctx context.Context, root, query string, paths []string, limit int) ([]Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	files, err := rootio.ListFilesRecursive(paths)
	if err != nil {
		return nil, err
	}
	index, err := bleve.NewMemOnly(bleve.NewIndexMapping())
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = index.Close()
	}()
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rel, err := rootio.RelUnderRoot(root, f)
		if err != nil {
			rel = filepath.ToSlash(f)
		}
		mtime := ""
		if st, err := os.Stat(f); err == nil {
			mtime = st.ModTime().Format(time.RFC3339)
		}
		fh, err := os.Open(f)
		if err != nil {
			continue
		}
		s := bufio.NewScanner(fh)
		s.Buffer(make([]byte, 64*1024), maxScannerToken)
		ln := 0
		for s.Scan() {
			if err := ctx.Err(); err != nil {
				_ = fh.Close()
				return nil, err
			}
			ln++
			lineText := s.Text()
			doc := bleveLineDoc{
				File:    rel,
				Line:    ln,
				Preview: strings.TrimSpace(lineText),
				Content: lineText,
				Mtime:   mtime,
			}
			if err := index.Index(rel+":"+strconv.Itoa(ln), doc); err != nil {
				_ = fh.Close()
				return nil, err
			}
		}
		_ = fh.Close()
	}

	q := bleve.NewMatchQuery(query)
	q.SetField("content")
	size := limit
	if size <= 0 {
		size = 50
	}
	req := bleve.NewSearchRequestOptions(q, size, 0, false)
	req.Fields = []string{"file", "line", "preview", "mtime", "content"}
	res, err := index.SearchInContext(ctx, req)
	if err != nil {
		return nil, err
	}
	out := make([]Result, 0, len(res.Hits))
	for _, hit := range res.Hits {
		fields := hit.Fields
		file, _ := fields["file"].(string)
		preview, _ := fields["preview"].(string)
		mtime, _ := fields["mtime"].(string)
		content, _ := fields["content"].(string)
		line := int(numberField(fields["line"]))
		col := strings.Index(strings.ToLower(content), strings.ToLower(query)) + 1
		if col <= 0 {
			col = 1
		}
		out = append(out, Result{
			File:    file,
			Line:    line,
			Col:     col,
			Preview: preview,
			Mtime:   mtime,
		})
	}
	return out, nil
}

func numberField(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
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
