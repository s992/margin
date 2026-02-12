# Contributing

## Prerequisites

- Go (matching `cli-go/go.mod`)
- Python 3.11+
- `uv`
- Optional for release dry-runs: `goreleaser`

## Setup

```bash
uv sync --dev
uv run pre-commit install
```

## Development workflow

Use the root `Makefile` targets:

- `make fmt` formats Go and plugin Python code.
- `make lint` runs static checks.
- `make test` runs Go and plugin Python tests.
- `make check` runs lint + tests.
- `make build` builds the local CLI binary.

## Release workflow

Releases are created from GitHub Actions via **Release** workflow (`workflow_dispatch`).
Provide a SemVer tag input like `v0.1.0`. The workflow will:

1. Validate the tag format.
2. Create and push the annotated tag.
3. Build/publish release binaries with GoReleaser.
4. Publish release notes containing the commit list.

## Commit messages

This repository expects commit messages prefixed with:

- `ai slop: `
