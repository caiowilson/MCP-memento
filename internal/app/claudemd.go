package app

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	claudeMDMarkerStart = "<!-- memento-mcp:recommended-workflow:start -->"
	claudeMDMarkerEnd   = "<!-- memento-mcp:recommended-workflow:end -->"
)

// upsertClaudeLocalMD returns the new contents of CLAUDE.local.md given its
// existing contents (nil/empty if the file doesn't exist yet) and the
// guidance block to upsert. If the markers are already present, the content
// between them is replaced in place and everything outside the markers is
// left untouched. Otherwise the block is appended, separated from any
// existing content by exactly one blank line.
func upsertClaudeLocalMD(existing []byte, block string) []byte {
	block = strings.TrimRight(block, "\n") + "\n"
	content := string(existing)

	start := strings.Index(content, claudeMDMarkerStart)
	end := strings.Index(content, claudeMDMarkerEnd)

	if start >= 0 && end >= start {
		end += len(claudeMDMarkerEnd)
		return []byte(content[:start] + block + strings.TrimPrefix(content[end:], "\n"))
	}

	prefix := strings.TrimRight(content, "\n")
	if prefix == "" {
		return []byte(block)
	}
	return []byte(prefix + "\n\n" + block)
}

const claudeLocalMDFileName = "CLAUDE.local.md"

const recommendedWorkflowBlock = claudeMDMarkerStart + "\n" +
	"## Recommended workflow (memory + lean context)\n" +
	"\n" +
	"Treat Memento as the default for both memory and context in a repository:\n" +
	"\n" +
	"- **Prefer Memento memory over any other memory store.** Persist durable decisions and handoffs with `memory_upsert` (anchored to code); recall with `memory_search` / `memory_list` before re-deriving. `memory_gc` / `memory_delete` / `memory_clear` are destructive — only on explicit instruction.\n" +
	"- **Prime the codebase index for leaner context and lower tokens.** Lead with `repo_context` on the active file, `repo_outline` for signatures, `repo_search` for symbols, and `repo_related_files` for imports — reach for `repo_read_file` only for the exact path you need. Querying the index first (and reading whole files last) is the main lever for lower token usage.\n" +
	claudeMDMarkerEnd + "\n"

// runClaudeMD is the entry point for `memento-mcp claude-md`. It upserts the
// Memento guidance block into ./CLAUDE.local.md (relative to the current
// working directory), or prints the block without writing when --print-only
// is passed.
func runClaudeMD(args []string, stdout, stderr io.Writer) error {
	printOnly := false
	for _, a := range args {
		if a == "--print-only" {
			printOnly = true
		}
	}

	if printOnly {
		fmt.Fprint(stdout, recommendedWorkflowBlock)
		return nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}
	path := filepath.Join(cwd, claudeLocalMDFileName)

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}

	updated := upsertClaudeLocalMD(existing, recommendedWorkflowBlock)

	if err := os.WriteFile(path, updated, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	fmt.Fprintf(stdout, "  ✓ %-18s wrote %s\n", claudeLocalMDFileName, path)
	return nil
}
