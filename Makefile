GO_DIR := cli-go
PLUGIN_DIR := sublime-plugin/Margin
DIST_DIR := dist
PLUGIN_PACKAGE := $(DIST_DIR)/Margin.sublime-package

.PHONY: help fmt lint test check build clean release-snapshot plugin-package install \
	go-fmt go-fmt-check go-lint go-test py-lint py-format py-format-check py-test py-sync

help:
	@echo "Targets:"
	@echo "  fmt               Format Go and Python code"
	@echo "  lint              Run static analysis and format checks"
	@echo "  test              Run tests"
	@echo "  check             Run lint and tests"
	@echo "  build             Build margin CLI"
	@echo "  install           Install CLI + Sublime plugin from local source"
	@echo "  py-sync           Sync Python dev tooling via uv"
	@echo "  release-snapshot  Build release artifacts locally (no publish)"
	@echo "  plugin-package    Build Margin.sublime-package"
	@echo "  clean             Remove generated artifacts"

fmt: go-fmt py-format

lint: go-fmt-check go-lint py-lint py-format-check

test: go-test py-test

check: lint test

build:
	cd $(GO_DIR) && go build -o margin ./cmd/margin

install:
	@if grep -qiE 'microsoft|wsl' /proc/version 2>/dev/null; then \
		source_arg='--source local'; \
		if printf '%s' "$(INSTALL_ARGS)" | grep -q -- '--source'; then \
			source_arg=''; \
		fi; \
		if printf '%s' "$(INSTALL_ARGS)" | grep -q -- '--target-env'; then \
			./scripts/install.sh $$source_arg $(INSTALL_ARGS); \
		else \
			echo "WSL detected with no --target-env override: defaulting to Linux paths (--target-env linux)."; \
			echo "To install to Windows paths instead, run: make install INSTALL_ARGS='--target-env windows'"; \
			./scripts/install.sh $$source_arg --target-env linux $(INSTALL_ARGS); \
		fi; \
	else \
		source_arg='--source local'; \
		if printf '%s' "$(INSTALL_ARGS)" | grep -q -- '--source'; then \
			source_arg=''; \
		fi; \
		./scripts/install.sh $$source_arg $(INSTALL_ARGS); \
	fi

release-snapshot:
	goreleaser release --clean --snapshot --skip=publish

plugin-package:
	mkdir -p $(DIST_DIR)
	cd sublime-plugin/Margin && zip -r ../../$(PLUGIN_PACKAGE) . -x '*/__pycache__/*' '*.pyc'

clean:
	rm -rf $(DIST_DIR)
	rm -f $(GO_DIR)/margin $(GO_DIR)/margin.exe

go-fmt:
	cd $(GO_DIR) && gofmt -w .

go-fmt-check:
	cd $(GO_DIR) && test -z "$$(gofmt -l .)"

go-lint:
	cd $(GO_DIR) && go vet ./...

go-test:
	cd $(GO_DIR) && go test ./...

py-sync:
	uv sync --dev

py-lint:
	uv run ruff check $(PLUGIN_DIR)

py-format:
	uv run ruff format $(PLUGIN_DIR)

py-format-check:
	uv run ruff format --check $(PLUGIN_DIR)

py-test:
	uv run pytest -q $(PLUGIN_DIR) scripts/tests/install_contract_test.py
