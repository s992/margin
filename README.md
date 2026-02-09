# Margin

Local-first scratch workflow for Sublime Text backed by a Go CLI.

## AI-Generated Notice

This project was generated with AI assistance. Use it at your own risk.

Before using it for important or sensitive work, review the code and test it in your environment. AI-generated code can contain security vulnerabilities, data-loss bugs, incorrect behavior, and incomplete error handling.

## Repo layout

- `sublime-plugin/Margin/` Sublime plugin (thin UI + async subprocess calls)
- `cli-go/` Go CLI (`margin`) single-binary app

## Data root (defaults)

- Windows: `%APPDATA%/Margin`
- macOS: `~/Library/Application Support/Margin`
- Linux: `~/.local/share/margin`

Under root:

- `scratch/current/`
- `scratch/history/YYYY/YYYY-MM-DD/`
- `inbox/`
- `slack/`
- `index/reminders.json`
- `bin/margin(.exe)`
- `config.json`
- `logs/`

## Build CLI

```bash
cd cli-go
go build -o margin ./cmd/margin
```

Place the binary in one of:

1. Sublime setting `margin_cli_path`
2. `<root>/bin/margin(.exe)`
3. `PATH`

## Sublime install

Copy `sublime-plugin/Margin` into Sublime `Packages/` (or install manually via Package Control local package workflow).

Commands are exposed via Command Palette with `Margin:` prefix.

## CLI commands

```bash
margin search --query "foo" --root "<root>" [--paths scratch,inbox,slack] [--limit 50]
margin remind scan --root "<root>"
margin remind schedule --root "<root>"
margin run-block --file "<path>" --cursor 123 --root "<root>"
margin slack capture --channel C123 --thread 1712345678.000100 --root "<root>" --token-env SLACK_TOKEN --format markdown
margin mcp --transport stdio --root "<root>" [--readonly true|false]
```

All successful command results are JSON written to stdout.

## Config (`<root>/config.json`)

```json
{
  "autosave_interval_seconds": 5,
  "snapshot_interval_minutes": 10,
  "search_paths": ["scratch", "inbox", "slack"],
  "remind_enabled": false,
  "slack_enabled": false,
  "mcp_enabled": false,
  "mcp_readonly": true,
  "force_markdown_extension": true,
  "auto_replace_scratch_tab_with_file": true,
  "syntax_extension_map": {
    "Plain Text": "md",
    "Markdown": "md",
    "Python": "py",
    "JSON": "json",
    "Shell": "sh"
  },
  "runblock": {
    "python_bin": "python",
    "shell": "bash"
  }
}
```

## Notes

- Sublime plugin performs all file/network/CLI work in async contexts.
- Run-block requires an on-disk file; margin-managed scratch files are persisted under `scratch/current/`.
- Slack token is read from env (`SLACK_TOKEN` by default) and never written to disk.
