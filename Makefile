BIN_NAME := memento-mcp
BIN_DIR := bin
# Prefer Homebrew prefix on macOS if available; fallback to /usr/local
PREFIX ?= $(shell brew --prefix 2>/dev/null || echo /usr/local)
TARGET ?= server
REMOTE ?= origin
DRY_RUN ?= 0
CHECK_CLEAN ?= 1

# Parallel agent wave: worktree root and branch names
WAVE_ROOT   ?= $(shell dirname $(CURDIR))
WAVE_SLICES ?= 20 21 24 18
MERGE_ORDER ?= 20 21 24 18

.PHONY: build install install-dev uninstall clean help release release-server release-extension release-both \
        wave-status wave-validate wave-merge wave-clean

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
	@printf "\n"
	@printf "Parallel agent wave:\n"
	@printf "  wave-status    Show git status + diff --stat for each worktree\n"
	@printf "  wave-validate  Run the correct validation command in each worktree\n"
	@printf "  wave-merge     Merge each branch into current branch in MERGE_ORDER\n"
	@printf "  wave-clean     Remove worktrees + branches after successful merge\n"
	@printf "\n"
	@printf "Wave vars (override as needed):\n"
	@printf "  WAVE_ROOT=$(WAVE_ROOT)\n"
	@printf "  WAVE_SLICES=$(WAVE_SLICES)\n"
	@printf "  MERGE_ORDER=$(MERGE_ORDER)\n"

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

# ---------------------------------------------------------------------------
# Parallel agent wave helpers
# Each slice worktree lives at $(WAVE_ROOT)/memento-mcp-s<N>
# Each slice branch is named td/slice-<N>
# ---------------------------------------------------------------------------

# _wt_dir: resolve worktree directory for a given slice number
_wt_dir = $(WAVE_ROOT)/memento-mcp-s$(1)

wave-validate:
	@for s in $(WAVE_SLICES); do \
		dir="$(WAVE_ROOT)/memento-mcp-s$$s"; \
		echo ""; \
		echo "=== Slice $$s — $$dir ==="; \
		if [ ! -d "$$dir" ]; then \
			echo "  SKIP: worktree not found"; \
			continue; \
		fi; \
		(cd "$$dir" && case "$$s" in \
			20) go test ./internal/indexing/... ;; \
			21) go test ./... -cover ;; \
			24) echo "--- ref search ---" && (rg -l "README-old" . 2>/dev/null || echo "no references found") ;; \
			18) go test ./internal/mcp/... ;; \
			*)  go test ./... ;; \
		esac) || exit 1; \
	done

wave-status:
	@echo "=== Wave status ==="; \
	for s in $(WAVE_SLICES); do \
		dir="$(WAVE_ROOT)/memento-mcp-s$$s"; \
		echo ""; \
		echo "--- Slice $$s  ($$dir) ---"; \
		if [ ! -d "$$dir" ]; then \
			echo "  worktree not found — run: git worktree add $$dir -b td/slice-$$s"; \
		else \
			cd "$$dir" && git status -sb && echo "" && git diff --stat HEAD 2>/dev/null || true; \
		fi; \
	done

wave-merge:
	@CURRENT=$$(git rev-parse --abbrev-ref HEAD); \
	echo "=== Merging wave into $$CURRENT ==="; \
	FAILED=""; \
	for s in $(MERGE_ORDER); do \
		branch="td/slice-$$s"; \
		dir="$(WAVE_ROOT)/memento-mcp-s$$s"; \
		echo ""; \
		echo "--- Slice $$s (branch: $$branch) ---"; \
		if ! git rev-parse --verify "$$branch" >/dev/null 2>&1; then \
			echo "  SKIP: branch $$branch not found"; \
			continue; \
		fi; \
		if git merge --no-ff "$$branch" -m "merge td/slice-$$s"; then \
			echo "  merged ok"; \
		else \
			echo "  CONFLICT — fix manually, then run: git merge --continue"; \
			FAILED="$$FAILED $$s"; \
			break; \
		fi; \
	done; \
	if [ -n "$$FAILED" ]; then \
		echo ""; \
		echo "Wave merge stopped. Resolve conflicts for slice(s):$$FAILED"; \
		exit 1; \
	else \
		echo ""; \
		echo "All slices merged into $$CURRENT."; \
	fi

wave-clean:
	@echo "=== Cleaning wave worktrees and branches ==="; \
	for s in $(WAVE_SLICES); do \
		dir="$(WAVE_ROOT)/memento-mcp-s$$s"; \
		branch="td/slice-$$s"; \
		echo ""; \
		echo "--- Slice $$s ---"; \
		if git worktree list | grep -q "$$dir"; then \
			git worktree remove --force "$$dir" && echo "  removed worktree $$dir"; \
		else \
			echo "  worktree $$dir not found, skipping"; \
		fi; \
		if git rev-parse --verify "$$branch" >/dev/null 2>&1; then \
			git branch -d "$$branch" && echo "  deleted branch $$branch"; \
		else \
			echo "  branch $$branch not found, skipping"; \
		fi; \
	done
