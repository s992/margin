package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"margin/internal/config"
	"margin/internal/mcpserver"
	"margin/internal/remind"
	"margin/internal/rootio"
	"margin/internal/runblock"
	"margin/internal/search"
	"margin/internal/slackcap"
)

func main() {
	if len(os.Args) < 2 {
		fatalf(2, "usage: margin <subcommand>")
	}
	sub := os.Args[1]
	args := os.Args[2:]
	switch sub {
	case "search":
		handleSearch(args)
	case "remind":
		handleRemind(args)
	case "run-block":
		handleRunBlock(args)
	case "slack":
		handleSlack(args)
	case "mcp":
		handleMCP(args)
	default:
		fatalf(2, "unknown subcommand: %s", sub)
	}
}

func handleSearch(args []string) {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	query := fs.String("query", "", "query")
	paths := fs.String("paths", "", "comma paths")
	limit := fs.Int("limit", 50, "limit")
	root := fs.String("root", rootio.DefaultRoot(), "root")
	configPath := fs.String("config", "", "config path")
	_ = fs.Parse(args)
	cfg, _, err := config.Load(*root, *configPath)
	if err != nil {
		fatalf(1, "load config: %v", err)
	}
	if err := rootio.EnsureLayout(*root); err != nil {
		fatalf(1, "ensure layout: %v", err)
	}
	groups := cfg.SearchPaths
	if strings.TrimSpace(*paths) != "" {
		groups = splitCSV(*paths)
	}
	res, err := search.Run(*root, *query, groups, *limit)
	if err != nil {
		fatalf(1, "search: %v", err)
	}
	writeJSON(res)
}

func handleRemind(args []string) {
	if len(args) < 1 {
		fatalf(2, "usage: margin remind <scan|schedule>")
	}
	sub := args[0]
	fs := flag.NewFlagSet("remind "+sub, flag.ExitOnError)
	root := fs.String("root", rootio.DefaultRoot(), "root")
	configPath := fs.String("config", "", "config path")
	includeHistory := fs.Bool("include-history", false, "include scratch history")
	notify := fs.Bool("notify", true, "attempt desktop notifications")
	_ = fs.Parse(args[1:])
	_, _, err := config.Load(*root, *configPath)
	if err != nil {
		fatalf(1, "load config: %v", err)
	}
	if err := rootio.EnsureLayout(*root); err != nil {
		fatalf(1, "ensure layout: %v", err)
	}
	switch sub {
	case "scan":
		res, err := remind.Scan(*root, *includeHistory)
		if err != nil {
			fatalf(1, "remind scan: %v", err)
		}
		writeJSON(res)
	case "schedule":
		res, err := remind.Schedule(*root, *notify)
		if err != nil {
			fatalf(1, "remind schedule: %v", err)
		}
		writeJSON(res)
	default:
		fatalf(2, "unknown remind subcommand: %s", sub)
	}
}

func handleRunBlock(args []string) {
	fs := flag.NewFlagSet("run-block", flag.ExitOnError)
	file := fs.String("file", "", "file path")
	cursor := fs.String("cursor", "0", "cursor offset")
	root := fs.String("root", rootio.DefaultRoot(), "root")
	configPath := fs.String("config", "", "config path")
	_ = fs.Parse(args)
	cfg, _, err := config.Load(*root, *configPath)
	if err != nil {
		fatalf(1, "load config: %v", err)
	}
	if *file == "" {
		fatalf(2, "--file required")
	}
	cur, err := strconv.Atoi(*cursor)
	if err != nil {
		fatalf(2, "invalid --cursor: %v", err)
	}
	res, err := runblock.Run(*file, cur, cfg.RunBlock)
	if err != nil {
		fatalf(1, "run-block: %v", err)
	}
	writeJSON(res)
}

func handleSlack(args []string) {
	if len(args) < 1 {
		fatalf(2, "usage: margin slack capture")
	}
	sub := args[0]
	if sub != "capture" {
		fatalf(2, "unknown slack subcommand: %s", sub)
	}
	fs := flag.NewFlagSet("slack capture", flag.ExitOnError)
	channel := fs.String("channel", "", "channel id or name")
	thread := fs.String("thread", "", "thread ts or link")
	tokenEnv := fs.String("token-env", "SLACK_TOKEN", "token env var")
	format := fs.String("format", "markdown", "markdown|text")
	root := fs.String("root", rootio.DefaultRoot(), "root")
	configPath := fs.String("config", "", "config path")
	_ = fs.Parse(args[1:])
	_, _, err := config.Load(*root, *configPath)
	if err != nil {
		fatalf(1, "load config: %v", err)
	}
	if err := rootio.EnsureLayout(*root); err != nil {
		fatalf(1, "ensure layout: %v", err)
	}
	res, err := slackcap.Capture(*root, *channel, *thread, *tokenEnv, *format)
	if err != nil {
		fatalf(1, "slack capture: %v", err)
	}
	writeJSON(res)
}

func handleMCP(args []string) {
	fs := flag.NewFlagSet("mcp", flag.ExitOnError)
	transport := fs.String("transport", "stdio", "transport")
	readonly := fs.String("readonly", "", "true|false")
	root := fs.String("root", rootio.DefaultRoot(), "root")
	configPath := fs.String("config", "", "config path")
	_ = fs.Parse(args)
	cfg, _, err := config.Load(*root, *configPath)
	if err != nil {
		fatalf(1, "load config: %v", err)
	}
	if *transport != "stdio" {
		fatalf(2, "unsupported transport: %s", *transport)
	}
	if err := rootio.EnsureLayout(*root); err != nil {
		fatalf(1, "ensure layout: %v", err)
	}
	ro := cfg.MCPReadonly
	if *readonly != "" {
		v, err := strconv.ParseBool(*readonly)
		if err != nil {
			fatalf(2, "invalid --readonly value")
		}
		ro = v
	}
	if !cfg.MCPEnabled && *readonly == "" {
		fatalf(1, "mcp disabled in config; set mcp_enabled=true or pass --readonly explicitly to override")
	}
	srv := mcpserver.New(*root, ro, cfg.SearchPaths)
	if err := srv.Run(); err != nil {
		fatalf(1, "mcp server: %v", err)
	}
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func writeJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		fatalf(1, "encode json: %v", err)
	}
}

func fatalf(code int, format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(code)
}
