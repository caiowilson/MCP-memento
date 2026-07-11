package mcp

import "memento-mcp/internal/gitstate"

func loadGitIgnored(root string) *gitstate.IgnoredPaths {
	return gitstate.LoadIgnoredPaths(root)
}
