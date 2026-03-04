BIN_NAME := memento-mcp
BIN_DIR := bin
# Prefer Homebrew prefix on macOS if available; fallback to /usr/local
PREFIX ?= $(shell brew --prefix 2>/dev/null || echo /usr/local)
TARGET ?= server
REMOTE ?= origin
DRY_RUN ?= 0
CHECK_CLEAN ?= 1

.PHONY: build install install-dev uninstall clean help release release-server release-extension release-both

help:
	@printf "Targets:\n"
	@printf "  build     Build ./cmd/server into ./bin/$(BIN_NAME)\n"
	@printf "  install   Install to $(PREFIX)/bin/$(BIN_NAME)\n"
	@printf "  install-dev Install to $$HOME/.local/bin/$(BIN_NAME)\n"
	@printf "  uninstall Remove $(PREFIX)/bin/$(BIN_NAME)\n"
	@printf "  clean     Remove build artifacts\n"
	@printf "  release   Create and push release tags (server/extension/both)\n"
	@printf "  release-server|release-extension|release-both Convenience release wrappers\n"
	@printf "\n"
	@printf "Release vars:\n"
	@printf "  VERSION=<semver> (required, e.g. 0.3.0)\n"
	@printf "  TARGET=server|extension|both (default: server)\n"
	@printf "  REMOTE=<git remote> (default: origin)\n"
	@printf "  DRY_RUN=0|1 (default: 0)\n"
	@printf "  CHECK_CLEAN=0|1 (default: 1)\n"

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

release:
	@if [ -z "$(VERSION)" ]; then \
		echo "VERSION is required. Example: make release VERSION=0.3.0 [TARGET=server|extension|both]"; \
		exit 1; \
	fi
	@case "$(TARGET)" in \
		server|extension|both) ;; \
		*) echo "Invalid TARGET='$(TARGET)'. Use server, extension, or both."; exit 1 ;; \
	esac
	@if [ "$(CHECK_CLEAN)" = "1" ] && [ -n "$$(git status --porcelain)" ]; then \
		echo "Working tree is not clean. Commit/stash changes or run with CHECK_CLEAN=0."; \
		exit 1; \
	fi
	@TAGS=""; \
	case "$(TARGET)" in \
		server) TAGS="server/v$(VERSION)" ;; \
		extension) TAGS="extension/v$(VERSION)" ;; \
		both) TAGS="server/v$(VERSION) extension/v$(VERSION)" ;; \
	esac; \
	for tag in $$TAGS; do \
		if git rev-parse "$$tag" >/dev/null 2>&1; then \
			echo "Tag already exists locally: $$tag"; \
			exit 1; \
		fi; \
	done; \
	echo "Preparing release tags: $$TAGS"; \
	if [ "$(DRY_RUN)" = "1" ]; then \
		for tag in $$TAGS; do echo "git tag $$tag"; done; \
		echo "git push $(REMOTE) $$TAGS"; \
	else \
		for tag in $$TAGS; do git tag "$$tag"; done; \
		git push "$(REMOTE)" $$TAGS; \
		echo "Done. GitHub Actions will publish release assets for pushed tags."; \
	fi

release-server:
	@$(MAKE) release TARGET=server VERSION="$(VERSION)" REMOTE="$(REMOTE)" DRY_RUN="$(DRY_RUN)" CHECK_CLEAN="$(CHECK_CLEAN)"

release-extension:
	@$(MAKE) release TARGET=extension VERSION="$(VERSION)" REMOTE="$(REMOTE)" DRY_RUN="$(DRY_RUN)" CHECK_CLEAN="$(CHECK_CLEAN)"

release-both:
	@$(MAKE) release TARGET=both VERSION="$(VERSION)" REMOTE="$(REMOTE)" DRY_RUN="$(DRY_RUN)" CHECK_CLEAN="$(CHECK_CLEAN)"
