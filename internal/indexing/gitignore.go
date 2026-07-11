package indexing

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"

	gitignore "github.com/sabhiram/go-gitignore"
	"memento-mcp/internal/gitstate"
)

type ignoreRules struct {
	matcher    *gitignore.GitIgnore // .mementoignore, plus root .gitignore outside Git
	gitIgnored *gitstate.IgnoredPaths
}

// loadIgnoreRules combines Git's authoritative standard excludes with
// .mementoignore. Memento negations cannot re-include a Git-ignored path.
func loadIgnoreRules(root string) (*ignoreRules, error) {
	gitIgnored := gitstate.LoadIgnoredPaths(root)
	var allLines []string
	names := []string{".mementoignore"}
	if !gitIgnored.Available() {
		// Preserve root .gitignore behavior in non-Git workspaces.
		names = append([]string{".gitignore"}, names...)
	}
	for _, name := range names {
		lines, err := readIgnoreLines(filepath.Join(root, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		allLines = append(allLines, lines...)
	}
	if len(allLines) == 0 {
		return &ignoreRules{gitIgnored: gitIgnored}, nil
	}
	return &ignoreRules{matcher: gitignore.CompileIgnoreLines(allLines...), gitIgnored: gitIgnored}, nil
}

func readIgnoreLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines []string
	s := bufio.NewScanner(f)
	for s.Scan() {
		lines = append(lines, s.Text())
	}
	return lines, s.Err()
}

// matchesPath returns true if the slash-separated relative path should be ignored.
func (r *ignoreRules) matchesPath(relPath string) bool {
	if r == nil {
		return false
	}
	if r.gitIgnored.Matches(relPath) {
		return true
	}
	return r.matcher != nil && r.matcher.MatchesPath(relPath)
}
