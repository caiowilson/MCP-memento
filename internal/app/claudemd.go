package app

import "strings"

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
