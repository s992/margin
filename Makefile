GO_DIR := cli-go
PLUGIN_DIR := sublime-plugin/Margin
DIST_DIR := dist
PLUGIN_PACKAGE := $(DIST_DIR)/Margin.sublime-package

.PHONY: help fmt lint test check build clean release-snapshot plugin-package \
	go-fmt go-fmt-check go-lint go-test py-lint py-format py-format-check

help:
	@echo "Targets:"
	@echo "  fmt               Format Go and Python code"
	@echo "  lint              Run static analysis and format checks"
	@echo "  test              Run tests"
	@echo "  check             Run lint and tests"
	@echo "  build             Build margin CLI"
	@echo "  release-snapshot  Build release artifacts locally (no publish)"
	@echo "  plugin-package    Build Margin.sublime-package"
	@echo "  clean             Remove generated artifacts"

fmt: go-fmt py-format

lint: go-fmt-check go-lint py-lint py-format-check

test: go-test

check: lint test

build:
	cd $(GO_DIR) && go build -o margin ./cmd/margin

release-snapshot:
	goreleaser release --clean --snapshot --skip=publish

plugin-package:
	mkdir -p $(DIST_DIR)
	cd sublime-plugin && zip -r ../$(PLUGIN_PACKAGE) Margin -x '*/__pycache__/*' '*.pyc'

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

py-lint:
	ruff check $(PLUGIN_DIR)

py-format:
	ruff format $(PLUGIN_DIR)

py-format-check:
	ruff format --check $(PLUGIN_DIR)
