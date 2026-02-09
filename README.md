# Margin

Local-first scratch workflow for Sublime Text backed by a Go CLI.

## Repository layout

- `cli-go/`: Go CLI (`margin`) code and tests
- `sublime-plugin/Margin/`: Sublime Text plugin
- `.github/workflows/`: CI and release automation

## Project status

The repository currently does not declare an open-source license.

## Prerequisites

- Go (see `cli-go/go.mod`)
- Python 3.11+
- `ruff`
- `pre-commit`

## Developer quickstart

```bash
pre-commit install
make check
make build
```

Root-level commands:

- `make fmt`: format Go and Python
- `make lint`: run static checks and format checks
- `make test`: run tests
- `make check`: lint + test
- `make build`: build local CLI
- `make plugin-package`: build `dist/Margin.sublime-package`
- `make release-snapshot`: local GoReleaser dry run (no publish)

## CLI install and plugin wiring

Build locally:

```bash
make build
```

The plugin locates the CLI in this order:

1. Sublime setting `margin_cli_path`
2. `<margin_root>/bin/margin` (or `margin.exe` on Windows)
3. `PATH`

## CLI commands

```bash
margin version
margin search --query "foo" --root "<root>" [--paths scratch,inbox,slack] [--limit 50]
margin remind scan --root "<root>"
margin remind schedule --root "<root>"
margin run-block --file "<path>" --cursor 123 --root "<root>"
margin slack capture --channel C123 --thread 1712345678.000100 --root "<root>" --token-env SLACK_TOKEN --format markdown
margin mcp --transport stdio --root "<root>" [--readonly true|false]
```

## Release process

Official releases are created manually with GitHub Actions workflow **Release**.

Required input:

- `version`: SemVer tag (example: `v0.1.0`)

The workflow will create and push the tag, build binaries, and create a GitHub release
with generated release notes that include commits in the release window.

## Release binaries

Release artifacts are built for:

- Linux: `amd64`, `arm64`
- macOS: `amd64`, `arm64`
- Windows: `amd64`, `arm64`

<!-- temporary ci smoke test marker -->
