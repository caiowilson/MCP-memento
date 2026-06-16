package indexing

import (
	"bufio"
	"os"
	"path/filepath"

	gitignore "github.com/sabhiram/go-gitignore"
)

type ignoreRules struct {
	matcher *gitignore.GitIgnore // nil when no rules
}

// loadIgnoreRules reads .gitignore then .mementoignore from root,
// merging all lines in order so .mementoignore negations override .gitignore.
// Missing files are silently skipped.
func loadIgnoreRules(root string) (*ignoreRules, error) {
	var allLines []string
	for _, name := range []string{".gitignore", ".mementoignore"} {
		lines, err := readIgnoreLines(filepath.Join(root, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		allLines = append(allLines, lines...)
	}
	if len(allLines) == 0 {
		return &ignoreRules{}, nil
	}
	return &ignoreRules{matcher: gitignore.CompileIgnoreLines(allLines...)}, nil
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
	if r == nil || r.matcher == nil {
		return false
	}
	return r.matcher.MatchesPath(relPath)
}
