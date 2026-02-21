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
- `uv`

## Developer quickstart

```bash
uv sync --dev
uv run pre-commit install
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

One-command installer (POSIX shell):

```bash
curl -fsSL https://raw.githubusercontent.com/s992/margin/main/scripts/install.sh | bash
```

One-command installer (PowerShell):

```powershell
irm https://raw.githubusercontent.com/s992/margin/main/scripts/install.ps1 | iex
```

Repo-local install (developer workflow):

```bash
make install
```

Note for WSL users:
- `make install` defaults to Linux-target install to avoid interactive prompts.
- To target Windows Sublime/CLI paths from WSL, run:
  `make install INSTALL_ARGS="--target-env windows"`

The plugin locates the CLI in this order:

1. Sublime setting `margin_cli_path`
2. `<margin_root>/bin/margin` (or `margin.exe` on Windows)
3. `PATH`

Installer defaults:

- Source: latest GitHub release assets (`--source release`)
- Target env/arch: auto-detected (`--target-env auto --target-arch auto`)
- WSL behavior: installer prompts for `windows` vs `linux` target; in non-interactive mode it requires explicit `--target-env`
- Plugin mode: `.sublime-package` install into `Installed Packages` (`--plugin-mode package`)
- Settings: installer merges minimal keys into `User/Margin.sublime-settings` (`margin_cli_path`, optional `margin_root`)

Common overrides:

```bash
# pin version
./scripts/install.sh --version v0.1.0

# force source/local build
./scripts/install.sh --source local

# explicit target (required in non-interactive WSL auto mode)
./scripts/install.sh --target-env windows

# custom margin root + CLI path
./scripts/install.sh --margin-root "/tmp/margin-root" --cli-path "/tmp/margin-root/bin/margin"

# custom Sublime directories
./scripts/install.sh \
  --sublime-installed-packages-dir "$HOME/.config/sublime-text/Installed Packages" \
  --sublime-user-dir "$HOME/.config/sublime-text/Packages/User"

# install unpacked plugin source into Packages/Margin (repo/local mode)
./scripts/install.sh --source local --plugin-mode unpacked
```

WSL-to-Windows example:

```bash
./scripts/install.sh \
  --target-env windows \
  --source release
```

Troubleshooting:

- If release install fails in `--source release`, verify that the requested version has both CLI archive and `Margin.sublime-package` assets.
- If using `--source auto`, the installer falls back to local build when release download is unavailable.
- If existing `Margin.sublime-settings` is not valid JSON, installer aborts instead of overwriting unknown content.

## CLI commands

```bash
margin version
margin search --query "foo" --root "<root>" [--paths scratch,inbox,slack] [--limit 50]
margin remind scan --root "<root>"
margin remind schedule --root "<root>"
margin run-block --file "<path>" --cursor 123 --root "<root>"
margin slack capture --transcript "sean  [10:48 AM]\nhello" --root "<root>" --format markdown
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
