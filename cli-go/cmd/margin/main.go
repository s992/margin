package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

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

type cliError struct {
	code int
	msg  string
}

func (e cliError) Error() string {
	return e.msg
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(os.Args) >= 2 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		writeVersionJSON()
		return
	}

	cmd := newRootCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	if err := cmd.ExecuteContext(ctx); err != nil {
		var ce cliError
		if ok := errorAs(err, &ce); ok {
			fatalf(ce.code, "%s", ce.msg)
		}
		if strings.HasPrefix(err.Error(), "unknown command") {
			toks := strings.Fields(err.Error())
			if len(toks) >= 3 {
				unknown := strings.Trim(toks[2], `"`)
				fatalf(2, "unknown subcommand: %s", unknown)
			}
			fatalf(2, "%v", err)
		}
		fatalf(2, "%v", err)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:  "margin",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			writeVersionJSON()
			return nil
		},
	}

	root.AddCommand(newVersionCmd())
	root.AddCommand(newSearchCmd())
	root.AddCommand(newRemindCmd())
	root.AddCommand(newRunBlockCmd())
	root.AddCommand(newSlackCmd())
	root.AddCommand(newMCPCmd())
	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print build metadata",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			writeVersionJSON()
			return nil
		},
	}
}

func newSearchCmd() *cobra.Command {
	var query string
	var paths string
	var limit int
	var root string
	var configPath string

	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search notes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfigAndLayout(root, configPath)
			if err != nil {
				return err
			}
			groups := cfg.SearchPaths
			if strings.TrimSpace(paths) != "" {
				groups = splitCSV(paths)
			}
			res, err := search.Run(cmd.Context(), root, query, groups, limit)
			if err != nil {
				return cliError{code: 1, msg: fmt.Sprintf("search: %v", err)}
			}
			writeJSON(res)
			return nil
		},
	}
	cmd.Flags().StringVar(&query, "query", "", "query")
	cmd.Flags().StringVar(&paths, "paths", "", "comma paths")
	cmd.Flags().IntVar(&limit, "limit", 50, "limit")
	cmd.Flags().StringVar(&root, "root", rootio.DefaultRoot(), "root")
	cmd.Flags().StringVar(&configPath, "config", "", "config path")
	return cmd
}

func newRemindCmd() *cobra.Command {
	var root string
	var configPath string
	var includeHistory bool
	var notify bool

	remindCmd := &cobra.Command{
		Use:   "remind",
		Short: "Reminder operations",
	}
	remindCmd.PersistentFlags().StringVar(&root, "root", rootio.DefaultRoot(), "root")
	remindCmd.PersistentFlags().StringVar(&configPath, "config", "", "config path")

	scanCmd := &cobra.Command{
		Use:   "scan",
		Short: "Scan notes for reminders",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := loadConfigAndLayout(root, configPath); err != nil {
				return err
			}
			res, err := remind.Scan(cmd.Context(), root, includeHistory)
			if err != nil {
				return cliError{code: 1, msg: fmt.Sprintf("remind scan: %v", err)}
			}
			writeJSON(res)
			return nil
		},
	}
	scanCmd.Flags().BoolVar(&includeHistory, "include-history", false, "include scratch history")

	scheduleCmd := &cobra.Command{
		Use:   "schedule",
		Short: "Run reminder scheduler",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := loadConfigAndLayout(root, configPath); err != nil {
				return err
			}
			res, err := remind.Schedule(cmd.Context(), root, notify)
			if err != nil {
				return cliError{code: 1, msg: fmt.Sprintf("remind schedule: %v", err)}
			}
			writeJSON(res)
			return nil
		},
	}
	scheduleCmd.Flags().BoolVar(&notify, "notify", true, "attempt desktop notifications")

	remindCmd.AddCommand(scanCmd, scheduleCmd)
	return remindCmd
}

func newRunBlockCmd() *cobra.Command {
	var file string
	var cursor string
	var root string
	var configPath string

	cmd := &cobra.Command{
		Use:   "run-block",
		Short: "Run fenced code block at cursor",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(root, configPath)
			if err != nil {
				return err
			}
			if file == "" {
				return cliError{code: 2, msg: "--file required"}
			}
			cur, err := strconv.Atoi(cursor)
			if err != nil {
				return cliError{code: 2, msg: fmt.Sprintf("invalid --cursor: %v", err)}
			}
			res, err := runblock.Run(cmd.Context(), file, cur, cfg.RunBlock)
			if err != nil {
				return cliError{code: 1, msg: fmt.Sprintf("run-block: %v", err)}
			}
			writeJSON(res)
			return nil
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "file path")
	cmd.Flags().StringVar(&cursor, "cursor", "0", "cursor offset")
	cmd.Flags().StringVar(&root, "root", rootio.DefaultRoot(), "root")
	cmd.Flags().StringVar(&configPath, "config", "", "config path")
	return cmd
}

func newSlackCmd() *cobra.Command {
	var channel string
	var thread string
	var tokenEnv string
	var format string
	var root string
	var configPath string

	slackCmd := &cobra.Command{
		Use:   "slack",
		Short: "Slack capture commands",
	}

	captureCmd := &cobra.Command{
		Use:   "capture",
		Short: "Capture Slack thread",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := loadConfigAndLayout(root, configPath); err != nil {
				return err
			}
			res, err := slackcap.Capture(cmd.Context(), root, channel, thread, tokenEnv, format)
			if err != nil {
				return cliError{code: 1, msg: fmt.Sprintf("slack capture: %v", err)}
			}
			writeJSON(res)
			return nil
		},
	}
	captureCmd.Flags().StringVar(&channel, "channel", "", "channel id or name")
	captureCmd.Flags().StringVar(&thread, "thread", "", "thread ts or link")
	captureCmd.Flags().StringVar(&tokenEnv, "token-env", "SLACK_TOKEN", "token env var")
	captureCmd.Flags().StringVar(&format, "format", "markdown", "markdown|text")
	captureCmd.Flags().StringVar(&root, "root", rootio.DefaultRoot(), "root")
	captureCmd.Flags().StringVar(&configPath, "config", "", "config path")

	slackCmd.AddCommand(captureCmd)
	return slackCmd
}

func newMCPCmd() *cobra.Command {
	var transport string
	var readonly string
	var root string
	var configPath string

	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run MCP server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfigAndLayout(root, configPath)
			if err != nil {
				return err
			}
			if transport != "stdio" {
				return cliError{code: 2, msg: fmt.Sprintf("unsupported transport: %s", transport)}
			}
			ro := cfg.MCPReadonly
			if readonly != "" {
				v, err := strconv.ParseBool(readonly)
				if err != nil {
					return cliError{code: 2, msg: "invalid --readonly value"}
				}
				ro = v
			}
			if !cfg.MCPEnabled && readonly == "" {
				return cliError{code: 1, msg: "mcp disabled in config; set mcp_enabled=true or pass --readonly explicitly to override"}
			}
			srv := mcpserver.New(root, ro, cfg.SearchPaths)
			if err := srv.Run(cmd.Context()); err != nil {
				return cliError{code: 1, msg: fmt.Sprintf("mcp server: %v", err)}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&transport, "transport", "stdio", "transport")
	cmd.Flags().StringVar(&readonly, "readonly", "", "true|false")
	cmd.Flags().StringVar(&root, "root", rootio.DefaultRoot(), "root")
	cmd.Flags().StringVar(&configPath, "config", "", "config path")
	return cmd
}

func loadConfig(root, configPath string) (config.Config, error) {
	cfg, _, err := config.Load(root, configPath)
	if err != nil {
		return config.Config{}, cliError{code: 1, msg: fmt.Sprintf("load config: %v", err)}
	}
	return cfg, nil
}

func loadConfigAndLayout(root, configPath string) (config.Config, error) {
	cfg, err := loadConfig(root, configPath)
	if err != nil {
		return config.Config{}, err
	}
	if err := rootio.EnsureLayout(root); err != nil {
		return config.Config{}, cliError{code: 1, msg: fmt.Sprintf("ensure layout: %v", err)}
	}
	return cfg, nil
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

func writeVersionJSON() {
	writeJSON(map[string]string{
		"version": version,
		"commit":  commit,
		"date":    date,
	})
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

func errorAs(err error, target *cliError) bool {
	if err == nil {
		return false
	}
	ce, ok := err.(cliError)
	if ok {
		*target = ce
		return true
	}
	return false
}
