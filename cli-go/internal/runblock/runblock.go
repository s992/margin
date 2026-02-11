package runblock

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/google/shlex"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"

	"margin/internal/config"
)

const executionTimeout = 30 * time.Second

type Block struct {
	Language     string
	Code         string
	Start        int
	End          int
	CodeStart    int
	CodeEnd      int
	FenceEndLine int
}

type Result struct {
	Language string `json:"language"`
	Output   string `json:"output"`
	ExitCode int    `json:"exit_code"`
	RanAt    string `json:"ran_at"`
	BlockEnd int    `json:"block_end"`
}

func Run(ctx context.Context, filePath string, cursor int, cfg config.RunBlockConfig) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	b, err := os.ReadFile(filePath)
	if err != nil {
		return Result{}, err
	}
	blocks := ParseBlocks(string(b))
	if len(blocks) == 0 {
		return Result{}, errors.New("no fenced code block found")
	}
	block := PickBlock(blocks, cursor)
	if block == nil {
		return Result{}, errors.New("unable to select code block")
	}

	lang := strings.ToLower(block.Language)
	res := Result{Language: lang, RanAt: time.Now().Format(time.RFC3339), BlockEnd: block.End}
	switch lang {
	case "bash", "sh", "shell":
		output, code := runShell(ctx, block.Code, cfg.Shell)
		res.Output = output
		res.ExitCode = code
	case "python", "py":
		output, code := runPython(ctx, block.Code, cfg.PythonBin)
		res.Output = output
		res.ExitCode = code
	case "json":
		pretty, err := prettyJSON(block.Code)
		if err != nil {
			res.Output = err.Error()
			res.ExitCode = 1
		} else {
			res.Output = pretty
			res.ExitCode = 0
		}
	case "sql":
		if strings.TrimSpace(cfg.SQLCmd) == "" {
			return Result{}, errors.New("sql execution unsupported without runblock.sql_cmd")
		}
		output, code := runWithCmd(ctx, cfg.SQLCmd, block.Code)
		res.Output = output
		res.ExitCode = code
	default:
		return Result{}, fmt.Errorf("unsupported language: %s", block.Language)
	}
	return res, nil
}

func ParseBlocks(s string) []Block {
	src := []byte(s)
	doc := goldmark.New().Parser().Parse(text.NewReader(src))
	blocks := make([]Block, 0)
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		fb, ok := n.(*ast.FencedCodeBlock)
		if !ok {
			return ast.WalkContinue, nil
		}
		lines := fb.Lines()
		code := string(lines.Value(src))
		code = strings.TrimSuffix(code, "\n")

		codeStart := 0
		codeEnd := 0
		if lines.Len() > 0 {
			codeStart = lines.At(0).Start
			codeEnd = lines.At(lines.Len() - 1).Stop
		}
		start := findOpeningFenceStart(src, codeStart)
		if start < 0 {
			start = codeStart
		}
		end := findClosingFenceEnd(src, codeEnd)
		if end < 0 {
			end = codeEnd
		}
		blocks = append(blocks, Block{
			Language:  string(fb.Language(src)),
			Code:      code,
			Start:     start,
			End:       end,
			CodeStart: codeStart,
			CodeEnd:   codeEnd,
		})
		return ast.WalkContinue, nil
	})
	return blocks
}

func findOpeningFenceStart(src []byte, codeStart int) int {
	if codeStart <= 0 {
		return 0
	}
	lineEnd := codeStart - 1
	for lineEnd >= 0 && (src[lineEnd] == '\n' || src[lineEnd] == '\r') {
		lineEnd--
	}
	if lineEnd < 0 {
		return 0
	}
	lineStart := lineEnd
	for lineStart > 0 && src[lineStart-1] != '\n' {
		lineStart--
	}
	if isFenceLine(string(src[lineStart : lineEnd+1])) {
		return lineStart
	}
	return lineStart
}

func findClosingFenceEnd(src []byte, codeEnd int) int {
	if codeEnd < 0 {
		codeEnd = 0
	}
	if codeEnd > len(src) {
		codeEnd = len(src)
	}
	idx := codeEnd
	for idx < len(src) {
		if idx > 0 && src[idx-1] != '\n' {
			next := bytes.IndexByte(src[idx:], '\n')
			if next < 0 {
				return len(src)
			}
			idx += next + 1
		}
		if idx >= len(src) {
			return len(src)
		}
		lineEnd := len(src)
		if next := bytes.IndexByte(src[idx:], '\n'); next >= 0 {
			lineEnd = idx + next
		}
		line := string(src[idx:lineEnd])
		if isFenceLine(line) {
			if lineEnd < len(src) {
				return lineEnd + 1
			}
			return lineEnd
		}
		if lineEnd == len(src) {
			return lineEnd
		}
		idx = lineEnd + 1
	}
	return len(src)
}

func isFenceLine(line string) bool {
	trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}

func PickBlock(blocks []Block, cursor int) *Block {
	if len(blocks) == 0 {
		return nil
	}
	for i := range blocks {
		if cursor >= blocks[i].Start && cursor <= blocks[i].End {
			return &blocks[i]
		}
	}
	type candidate struct {
		idx  int
		dist int
	}
	cands := make([]candidate, 0, len(blocks))
	for i := range blocks {
		d := blocks[i].Start - cursor
		if d < 0 {
			d = -d
		}
		cands = append(cands, candidate{idx: i, dist: d})
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].dist < cands[j].dist })
	return &blocks[cands[0].idx]
}

func runShell(ctx context.Context, code, shell string) (string, int) {
	candidates := shellCandidates(shell)
	lastErr := ""
	for _, sh := range candidates {
		output, exitCode, err := runShellWithBinary(ctx, sh, code)
		if err == nil {
			return output, exitCode
		}
		if isNotFoundErr(err) {
			lastErr = err.Error()
			continue
		}
		return output + "\n" + err.Error(), 1
	}
	if lastErr == "" {
		lastErr = "no shell found to run block"
	}
	return lastErr, 1
}

func runShellWithBinary(ctx context.Context, shell, code string) (string, int, error) {
	s := strings.TrimSpace(shell)
	if s == "" {
		return "", 1, errors.New("empty shell")
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, executionTimeout)
	defer cancel()

	var cmd *exec.Cmd
	switch strings.ToLower(s) {
	case "wsl.exe", "wsl":
		cmd = exec.CommandContext(timeoutCtx, s, "bash", "-lc", code)
	case "cmd.exe", "cmd":
		cmd = exec.CommandContext(timeoutCtx, s, "/C", code)
	default:
		cmd = exec.CommandContext(timeoutCtx, s, "-lc", code)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if err == nil {
		return out.String(), 0, nil
	}
	if errors.Is(timeoutCtx.Err(), context.DeadlineExceeded) {
		return out.String(), 124, fmt.Errorf("command timed out after %s", executionTimeout)
	}
	if errors.Is(timeoutCtx.Err(), context.Canceled) {
		return out.String(), 130, timeoutCtx.Err()
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return out.String(), ee.ExitCode(), nil
	}
	return out.String(), 1, err
}

func shellCandidates(configured string) []string {
	out := make([]string, 0, 8)
	if strings.TrimSpace(configured) != "" {
		out = append(out, configured)
	}
	out = append(out, "bash", "sh")
	if runtime.GOOS == "windows" {
		out = append(out,
			`C:\Program Files\Git\bin\bash.exe`,
			`C:\Program Files\Git\usr\bin\bash.exe`,
			"wsl.exe",
		)
	}
	return uniqueStrings(out)
}

func uniqueStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func isNotFoundErr(err error) bool {
	if errors.Is(err, exec.ErrNotFound) {
		return true
	}
	var pe *os.PathError
	if errors.As(err, &pe) {
		return errors.Is(pe.Err, os.ErrNotExist)
	}
	return false
}

func runPython(ctx context.Context, code, pythonBin string) (string, int) {
	if strings.TrimSpace(pythonBin) == "" {
		pythonBin = "python"
	}
	tmp, err := os.CreateTemp("", "margin-run-*.py")
	if err != nil {
		return err.Error(), 1
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()
	if _, err := tmp.WriteString(code); err != nil {
		_ = tmp.Close()
		return err.Error(), 1
	}
	if err := tmp.Close(); err != nil {
		return err.Error(), 1
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, executionTimeout)
	defer cancel()
	cmd := exec.CommandContext(timeoutCtx, pythonBin, tmpName)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err = cmd.Run()
	if err == nil {
		return out.String(), 0
	}
	if errors.Is(timeoutCtx.Err(), context.DeadlineExceeded) {
		return out.String() + "\ncommand timed out", 124
	}
	if errors.Is(timeoutCtx.Err(), context.Canceled) {
		return out.String() + "\ncommand canceled", 130
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return out.String(), ee.ExitCode()
	}
	return out.String() + "\n" + err.Error(), 1
}

func runWithCmd(ctx context.Context, command, input string) (string, int) {
	parts, err := shlex.Split(command)
	if err != nil {
		return "invalid command: " + err.Error(), 1
	}
	if len(parts) == 0 {
		return "invalid command", 1
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, executionTimeout)
	defer cancel()
	cmd := exec.CommandContext(timeoutCtx, parts[0], parts[1:]...)
	cmd.Stdin = strings.NewReader(input)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err = cmd.Run()
	if err == nil {
		return out.String(), 0
	}
	if errors.Is(timeoutCtx.Err(), context.DeadlineExceeded) {
		return out.String() + "\ncommand timed out", 124
	}
	if errors.Is(timeoutCtx.Err(), context.Canceled) {
		return out.String() + "\ncommand canceled", 130
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return out.String(), ee.ExitCode()
	}
	return out.String() + "\n" + err.Error(), 1
}

func prettyJSON(in string) (string, error) {
	var v any
	if err := json.Unmarshal([]byte(in), &v); err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}
