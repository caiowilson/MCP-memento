BIN_NAME := memento-mcp
BIN_DIR := bin
# Prefer Homebrew prefix on macOS if available; fallback to /usr/local
PREFIX ?= $(shell brew --prefix 2>/dev/null || echo /usr/local)

.PHONY: build install install-dev uninstall clean help

help:
	@printf "Targets:\n"
	@printf "  build     Build ./cmd/server into ./bin/$(BIN_NAME)\n"
	@printf "  install   Install to $(PREFIX)/bin/$(BIN_NAME)\n"
	@printf "  install-dev Install to $$HOME/.local/bin/$(BIN_NAME)\n"
	@printf "  uninstall Remove $(PREFIX)/bin/$(BIN_NAME)\n"
	@printf "  clean     Remove build artifacts\n"

build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(BIN_NAME) ./cmd/server

install: build
	@install -d $(PREFIX)/bin
	install -m 0755 $(BIN_DIR)/$(BIN_NAME) $(PREFIX)/bin/$(BIN_NAME)

install-dev: build
	@install -d "$$HOME/.local/bin"
	install -m 0755 $(BIN_DIR)/$(BIN_NAME) "$$HOME/.local/bin/$(BIN_NAME)"

uninstall:
	rm -f $(PREFIX)/bin/$(BIN_NAME)

clean:
	rm -rf $(BIN_DIR)
