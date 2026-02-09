package runblock

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/google/shlex"

	"margin/internal/config"
)

const executionTimeout = 30 * time.Second

var fenceRe = regexp.MustCompile("(?m)^[ \\t]{0,3}```([A-Za-z0-9_+-]*)[ \\t]*\\r?$")

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
	matches := fenceRe.FindAllStringSubmatchIndex(s, -1)
	if len(matches) == 0 {
		return nil
	}
	blocks := make([]Block, 0, len(matches)/2)
	for i := 0; i < len(matches); i++ {
		startIdx := matches[i][0]
		langStart, langEnd := matches[i][2], matches[i][3]
		lineEnd := strings.IndexByte(s[startIdx:], '\n')
		if lineEnd < 0 {
			continue
		}
		codeStart := startIdx + lineEnd + 1
		closingStart := findClosingFence(s, codeStart)
		if closingStart < 0 {
			continue
		}
		closingEnd := closingStart + strings.IndexByte(s[closingStart:], '\n')
		if closingEnd < closingStart {
			closingEnd = len(s)
		} else {
			closingEnd++
		}
		blocks = append(blocks, Block{
			Language:  s[langStart:langEnd],
			Code:      strings.TrimSuffix(s[codeStart:closingStart], "\n"),
			Start:     startIdx,
			End:       closingEnd,
			CodeStart: codeStart,
			CodeEnd:   closingStart,
		})
	}
	return blocks
}

func findClosingFence(s string, from int) int {
	idx := from
	for idx < len(s) {
		next := strings.IndexByte(s[idx:], '\n')
		lineEnd := len(s)
		if next >= 0 {
			lineEnd = idx + next
		}
		line := strings.TrimSpace(s[idx:lineEnd])
		if line == "```" {
			return idx
		}
		if next < 0 {
			break
		}
		idx = lineEnd + 1
	}
	return -1
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
