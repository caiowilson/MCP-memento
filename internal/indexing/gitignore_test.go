package indexing

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadIgnoreRulesMatchesGitignore(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("*.log\ndist/\n"), 0644); err != nil {
		t.Fatal(err)
	}
	rules, err := loadIgnoreRules(root)
	if err != nil {
		t.Fatal(err)
	}
	if !rules.matchesPath("app.log") {
		t.Error("expected app.log to be ignored")
	}
	if !rules.matchesPath("dist/bundle.js") {
		t.Error("expected dist/bundle.js to be ignored")
	}
	if rules.matchesPath("main.go") {
		t.Error("main.go should not be ignored")
	}
}

func TestLoadIgnoreRulesMementoignoreNegation(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("*.log\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".mementoignore"), []byte("!important.log\n"), 0644); err != nil {
		t.Fatal(err)
	}
	rules, err := loadIgnoreRules(root)
	if err != nil {
		t.Fatal(err)
	}
	if !rules.matchesPath("other.log") {
		t.Error("other.log should still be ignored")
	}
	if rules.matchesPath("important.log") {
		t.Error("important.log should be re-included by .mementoignore negation")
	}
}

func TestLoadIgnoreRulesNoFiles(t *testing.T) {
	root := t.TempDir()
	rules, err := loadIgnoreRules(root)
	if err != nil {
		t.Fatal(err)
	}
	if rules.matchesPath("anything.go") {
		t.Error("no rules defined — nothing should match")
	}
}
