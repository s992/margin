package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"margin/internal/config"
	"margin/internal/mcpserver"
	"margin/internal/remind"
	"margin/internal/rootio"
	"margin/internal/runblock"
	"margin/internal/search"
	"margin/internal/slackcap"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(os.Args) < 2 || os.Args[1] == "version" || os.Args[1] == "--version" || os.Args[1] == "-v" {
		writeJSON(map[string]string{
			"version": version,
			"commit":  commit,
			"date":    date,
		})
		return
	}
	sub := os.Args[1]
	args := os.Args[2:]
	switch sub {
	case "search":
		handleSearch(ctx, args)
	case "remind":
		handleRemind(ctx, args)
	case "run-block":
		handleRunBlock(ctx, args)
	case "slack":
		handleSlack(ctx, args)
	case "mcp":
		handleMCP(ctx, args)
	default:
		fatalf(2, "unknown subcommand: %s", sub)
	}
}

func handleSearch(ctx context.Context, args []string) {
	fs := newFlagSet("search")
	query := fs.String("query", "", "query")
	paths := fs.String("paths", "", "comma paths")
	limit := fs.Int("limit", 50, "limit")
	root := fs.String("root", rootio.DefaultRoot(), "root")
	configPath := fs.String("config", "", "config path")
	parseFlags(fs, args)

	cfg := loadConfigAndLayout(*root, *configPath)
	groups := cfg.SearchPaths
	if strings.TrimSpace(*paths) != "" {
		groups = splitCSV(*paths)
	}
	res, err := search.Run(ctx, *root, *query, groups, *limit)
	if err != nil {
		fatalf(1, "search: %v", err)
	}
	writeJSON(res)
}

func handleRemind(ctx context.Context, args []string) {
	if len(args) < 1 {
		fatalf(2, "usage: margin remind <scan|schedule>")
	}
	sub := args[0]
	fs := newFlagSet("remind " + sub)
	root := fs.String("root", rootio.DefaultRoot(), "root")
	configPath := fs.String("config", "", "config path")
	includeHistory := fs.Bool("include-history", false, "include scratch history")
	notify := fs.Bool("notify", true, "attempt desktop notifications")
	parseFlags(fs, args[1:])

	_ = loadConfigAndLayout(*root, *configPath)
	switch sub {
	case "scan":
		res, err := remind.Scan(ctx, *root, *includeHistory)
		if err != nil {
			fatalf(1, "remind scan: %v", err)
		}
		writeJSON(res)
	case "schedule":
		res, err := remind.Schedule(ctx, *root, *notify)
		if err != nil {
			fatalf(1, "remind schedule: %v", err)
		}
		writeJSON(res)
	default:
		fatalf(2, "unknown remind subcommand: %s", sub)
	}
}

func handleRunBlock(ctx context.Context, args []string) {
	fs := newFlagSet("run-block")
	file := fs.String("file", "", "file path")
	cursor := fs.String("cursor", "0", "cursor offset")
	root := fs.String("root", rootio.DefaultRoot(), "root")
	configPath := fs.String("config", "", "config path")
	parseFlags(fs, args)

	cfg := loadConfig(*root, *configPath)
	if *file == "" {
		fatalf(2, "--file required")
	}
	cur, err := strconv.Atoi(*cursor)
	if err != nil {
		fatalf(2, "invalid --cursor: %v", err)
	}
	res, err := runblock.Run(ctx, *file, cur, cfg.RunBlock)
	if err != nil {
		fatalf(1, "run-block: %v", err)
	}
	writeJSON(res)
}

func handleSlack(ctx context.Context, args []string) {
	if len(args) < 1 {
		fatalf(2, "usage: margin slack capture")
	}
	sub := args[0]
	if sub != "capture" {
		fatalf(2, "unknown slack subcommand: %s", sub)
	}
	fs := newFlagSet("slack capture")
	channel := fs.String("channel", "", "channel id or name")
	thread := fs.String("thread", "", "thread ts or link")
	tokenEnv := fs.String("token-env", "SLACK_TOKEN", "token env var")
	format := fs.String("format", "markdown", "markdown|text")
	root := fs.String("root", rootio.DefaultRoot(), "root")
	configPath := fs.String("config", "", "config path")
	parseFlags(fs, args[1:])

	_ = loadConfigAndLayout(*root, *configPath)
	res, err := slackcap.Capture(ctx, *root, *channel, *thread, *tokenEnv, *format)
	if err != nil {
		fatalf(1, "slack capture: %v", err)
	}
	writeJSON(res)
}

func handleMCP(ctx context.Context, args []string) {
	fs := newFlagSet("mcp")
	transport := fs.String("transport", "stdio", "transport")
	readonly := fs.String("readonly", "", "true|false")
	root := fs.String("root", rootio.DefaultRoot(), "root")
	configPath := fs.String("config", "", "config path")
	parseFlags(fs, args)

	cfg := loadConfigAndLayout(*root, *configPath)
	if *transport != "stdio" {
		fatalf(2, "unsupported transport: %s", *transport)
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
	if err := srv.Run(ctx); err != nil {
		fatalf(1, "mcp server: %v", err)
	}
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func parseFlags(fs *flag.FlagSet, args []string) {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fatalf(2, "%s", fs.Name())
		}
		fatalf(2, "%s", err)
	}
}

func loadConfig(root, configPath string) config.Config {
	cfg, _, err := config.Load(root, configPath)
	if err != nil {
		fatalf(1, "load config: %v", err)
	}
	return cfg
}

func loadConfigAndLayout(root, configPath string) config.Config {
	cfg := loadConfig(root, configPath)
	if err := rootio.EnsureLayout(root); err != nil {
		fatalf(1, "ensure layout: %v", err)
	}
	return cfg
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
