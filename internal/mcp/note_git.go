package mcp

import (
	"bytes"
	"os/exec"
	"strings"
)

func currentNoteGitState(root string) noteGitState {
	state := noteGitState{}
	if out, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output(); err == nil {
		state.Head = strings.TrimSpace(string(out))
	}
	if out, err := exec.Command("git", "-C", root, "symbolic-ref", "--quiet", "--short", "HEAD").Output(); err == nil {
		state.Branch = strings.TrimSpace(string(out))
	}
	return state
}

func noteAnchorSameLineage(root string, anchor NoteAnchor, current noteGitState) bool {
	if current.Head == "" {
		return false
	}
	if anchor.Branch != "" {
		if current.Branch == "" || anchor.Branch != current.Branch {
			return false
		}
	}
	if anchor.CommitSHA == "" {
		return anchor.Branch != "" && anchor.Branch == current.Branch
	}
	return exec.Command("git", "-C", root, "merge-base", "--is-ancestor", anchor.CommitSHA, current.Head).Run() == nil
}

func findNoteAnchorRename(root, commitSHA, oldPath string) string {
	if renamed := findWorkingTreeRename(root, oldPath); renamed != "" {
		return renamed
	}
	if commitSHA == "" {
		return ""
	}
	out, err := exec.Command("git", "-C", root, "diff", "--name-status", "-z", "-M", commitSHA, "HEAD").Output()
	if err != nil {
		return ""
	}
	parts := bytes.Split(out, []byte{0})
	for index := 0; index+2 < len(parts); index++ {
		status := string(parts[index])
		if !strings.HasPrefix(status, "R") && !strings.HasPrefix(status, "C") {
			continue
		}
		from := cleanNoteAnchorPath(string(parts[index+1]))
		to := cleanNoteAnchorPath(string(parts[index+2]))
		if from == oldPath {
			return to
		}
		index += 2
	}
	return ""
}

func findWorkingTreeRename(root, oldPath string) string {
	out, err := exec.Command("git", "-C", root, "status", "--porcelain=v1", "-z", "--untracked-files=all").Output()
	if err != nil {
		return ""
	}
	parts := bytes.Split(out, []byte{0})
	for index := 0; index+1 < len(parts); index++ {
		entry := parts[index]
		if len(entry) < 4 || (!bytes.Contains(entry[:2], []byte{'R'}) && !bytes.Contains(entry[:2], []byte{'C'})) {
			continue
		}
		to := cleanNoteAnchorPath(string(entry[3:]))
		from := cleanNoteAnchorPath(string(parts[index+1]))
		if from == oldPath {
			return to
		}
		index++
	}
	return ""
}
