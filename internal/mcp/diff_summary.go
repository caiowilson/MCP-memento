package mcp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"memento-mcp/internal/gitstate"
	"memento-mcp/internal/redact"
)

const (
	diffContextFormat          = "unified_diff"
	diffContextScopeStaged     = "staged"
	diffContextScopeUnstaged   = "unstaged"
	diffContextScopeUntracked  = "untracked"
	diffRedactionLookahead     = 64 * 1024
	diffCommandErrorMaxBytes   = 8 * 1024
	diffUntrackedReadExtraByte = 4
	diffCaptureOmittedMarker   = "[diff omitted: source exceeded safe redaction capture limit]\n"
)

type diffContextDiffSection struct {
	Scope     string `json:"scope"`
	Text      string `json:"text"`
	UsedBytes int    `json:"usedBytes"`
	Truncated bool   `json:"truncated"`
}

type diffContextDiffSummary struct {
	Available    bool                     `json:"available"`
	Format       string                   `json:"format"`
	ContextLines int                      `json:"contextLines"`
	MaxBytes     int                      `json:"maxBytes"`
	UsedBytes    int                      `json:"usedBytes"`
	Truncated    bool                     `json:"truncated"`
	Sections     []diffContextDiffSection `json:"sections"`
}

// buildDiffContextDiffSummary produces a deterministic, bounded view of the
// index, working tree, and untracked-file deltas represented by changes. The
// caller is responsible for filtering changes through the repository's path,
// ignore, and sensitive-file boundaries before calling this helper.
func buildDiffContextDiffSummary(
	ctx context.Context,
	root string,
	changes []gitstate.WorktreeChange,
	maxDiffBytes int,
	diffContextLines int,
	redactor *redact.Redactor,
) (diffContextDiffSummary, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return diffContextDiffSummary{}, err
	}
	if maxDiffBytes <= 0 {
		maxDiffBytes = defaultRepoDiffContextDiffBytes
	}
	if diffContextLines < 0 {
		diffContextLines = 0
	}
	if redactor == nil {
		redactor = redact.Default()
	}

	summary := diffContextDiffSummary{
		Format:       diffContextFormat,
		ContextLines: diffContextLines,
		MaxBytes:     maxDiffBytes,
		Sections:     make([]diffContextDiffSection, 0, 3),
	}
	available, err := diffSummaryGitAvailable(ctx, root)
	if err != nil {
		return summary, err
	}
	if !available {
		return summary, nil
	}
	summary.Available = true

	stagedPaths, unstagedPaths, untrackedPaths := diffSummaryPaths(changes)
	type requestedSection struct {
		scope string
		paths []string
	}
	requested := []requestedSection{
		{scope: diffContextScopeStaged, paths: stagedPaths},
		{scope: diffContextScopeUnstaged, paths: unstagedPaths},
		{scope: diffContextScopeUntracked, paths: untrackedPaths},
	}
	for _, candidate := range requested {
		if len(candidate.paths) == 0 {
			continue
		}
		var raw string
		var sourceTruncated bool
		if candidate.scope == diffContextScopeUntracked {
			raw, sourceTruncated, err = synthesizeUntrackedDiff(ctx, root, candidate.paths, diffRawCaptureLimit(maxDiffBytes))
		} else {
			raw, sourceTruncated, err = runBoundedGitDiff(
				ctx,
				root,
				candidate.paths,
				candidate.scope == diffContextScopeStaged,
				diffContextLines,
				diffRawCaptureLimit(maxDiffBytes),
				redactor,
			)
		}
		if err != nil {
			return summary, err
		}
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		if raw == "" && !sourceTruncated {
			continue
		}

		remaining := maxDiffBytes - summary.UsedBytes
		if remaining < 0 {
			remaining = 0
		}
		if sourceTruncated {
			raw = diffCaptureOmittedMarker
		}
		clean := strings.ToValidUTF8(raw, "")
		clean = redactor.Redact(clean)
		text := ""
		finalTruncated := clean != ""
		if remaining > 0 {
			text, finalTruncated = truncateStringBytes(clean, remaining)
		}
		section := diffContextDiffSection{
			Scope:     candidate.scope,
			Text:      text,
			UsedBytes: len(text),
			Truncated: sourceTruncated || finalTruncated,
		}
		summary.Sections = append(summary.Sections, section)
		summary.UsedBytes += section.UsedBytes
		summary.Truncated = summary.Truncated || section.Truncated
	}
	return summary, nil
}

func diffSummaryPaths(changes []gitstate.WorktreeChange) (staged, unstaged, untracked []string) {
	for _, change := range changes {
		implicit := !change.Staged && !change.Unstaged && !change.Untracked
		if change.Staged || implicit {
			staged = appendDiffSummaryChangePaths(staged, change)
		}
		if change.Unstaged || implicit {
			unstaged = appendDiffSummaryChangePaths(unstaged, change)
		}
		if change.Untracked {
			untracked = append(untracked, change.Path)
		}
	}
	return stableDiffPaths(staged), stableDiffPaths(unstaged), stableDiffPaths(untracked)
}

func appendDiffSummaryChangePaths(paths []string, change gitstate.WorktreeChange) []string {
	if change.Path != "" {
		paths = append(paths, change.Path)
	}
	if change.PreviousPath != "" {
		paths = append(paths, change.PreviousPath)
	}
	return paths
}

func stableDiffPaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		if _, duplicate := seen[path]; duplicate {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func diffSummaryGitAvailable(ctx context.Context, root string) (bool, error) {
	stdout := newBoundedDiffBuffer(16)
	stderr := newBoundedDiffBuffer(diffCommandErrorMaxBytes)
	cmd := exec.CommandContext(ctx, "git", "--literal-pathspecs", "-C", root, "rev-parse", "--is-inside-work-tree")
	cmd.Env = diffSummaryGitEnv()
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return false, ctxErr
	}
	if err != nil {
		return false, nil
	}
	return strings.TrimSpace(stdout.String()) == "true", nil
}

func runBoundedGitDiff(
	ctx context.Context,
	root string,
	paths []string,
	staged bool,
	contextLines int,
	captureLimit int,
	redactor *redact.Redactor,
) (string, bool, error) {
	args := []string{
		"--literal-pathspecs",
		"-C", root,
		"diff",
		"--no-ext-diff",
		"--no-textconv",
		"--no-color",
		"--no-renames",
		"--relative",
		"--src-prefix=a/",
		"--dst-prefix=b/",
		fmt.Sprintf("--unified=%d", contextLines),
	}
	if staged {
		args = append(args, "--cached")
	}
	args = append(args, "--")
	args = append(args, paths...)

	stdout := newBoundedDiffBuffer(captureLimit)
	stderr := newBoundedDiffBuffer(diffCommandErrorMaxBytes)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = diffSummaryGitEnv()
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", stdout.Truncated(), ctxErr
		}
		message := strings.TrimSpace(strings.ToValidUTF8(stderr.String(), ""))
		if stderr.Truncated() {
			message = "Git diff error output omitted because it exceeded the safety limit"
		}
		if message != "" {
			message = redactor.Redact(message)
			return "", stdout.Truncated(), fmt.Errorf("git diff: %w: %s", err, message)
		}
		return "", stdout.Truncated(), fmt.Errorf("git diff: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", stdout.Truncated(), err
	}
	return stdout.String(), stdout.Truncated(), nil
}

func diffSummaryGitEnv() []string {
	return append(os.Environ(),
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_LITERAL_PATHSPECS=1",
		"GIT_PAGER=cat",
		"LC_ALL=C",
	)
}

func synthesizeUntrackedDiff(ctx context.Context, root string, paths []string, captureLimit int) (string, bool, error) {
	out := newBoundedDiffBuffer(captureLimit)
	for _, rel := range paths {
		if err := ctx.Err(); err != nil {
			return "", out.Truncated(), err
		}
		file, err := openDiffRegularFile(root, rel)
		if err != nil {
			// The worktree can change between status discovery and diff capture.
			// Missing, replaced, symlinked, or special files are omitted safely.
			continue
		}
		readLimit := int64(captureLimit) + diffUntrackedReadExtraByte
		content, readErr := io.ReadAll(io.LimitReader(file, readLimit))
		closeErr := file.Close()
		if readErr != nil {
			return "", out.Truncated(), fmt.Errorf("read untracked diff source %s: %w", rel, readErr)
		}
		if closeErr != nil {
			return "", out.Truncated(), fmt.Errorf("close untracked diff source %s: %w", rel, closeErr)
		}
		if err := ctx.Err(); err != nil {
			return "", out.Truncated(), err
		}
		sourceTruncated := len(content) > captureLimit
		if sourceTruncated {
			content = []byte(prefixStringBytes(string(content), captureLimit))
			out.MarkTruncated()
		}

		from := quoteUnifiedDiffPath("a/" + filepath.ToSlash(rel))
		to := quoteUnifiedDiffPath("b/" + filepath.ToSlash(rel))
		_, _ = fmt.Fprintf(out, "diff --git %s %s\nnew file mode 100644\n--- /dev/null\n+++ %s\n", from, to, to)
		if bytes.IndexByte(content, 0) >= 0 || !utf8.Valid(content) {
			_, _ = fmt.Fprintf(out, "Binary files /dev/null and %s differ\n", to)
			continue
		}

		lineCount := unifiedDiffLineCount(content)
		_, _ = fmt.Fprintf(out, "@@ -0,0 +1,%d @@\n", lineCount)
		lines := bytes.SplitAfter(content, []byte{'\n'})
		for index, line := range lines {
			if len(line) == 0 && index == len(lines)-1 {
				continue
			}
			_, _ = out.Write([]byte{'+'})
			_, _ = out.Write(line)
			if len(line) == 0 || line[len(line)-1] != '\n' {
				_, _ = out.Write([]byte("\n\\ No newline at end of file\n"))
			}
		}
	}
	return out.String(), out.Truncated(), nil
}

func unifiedDiffLineCount(content []byte) int {
	if len(content) == 0 {
		return 0
	}
	lines := bytes.Count(content, []byte{'\n'})
	if content[len(content)-1] != '\n' {
		lines++
	}
	return lines
}

func quoteUnifiedDiffPath(path string) string {
	if strings.IndexFunc(path, func(r rune) bool {
		return r <= ' ' || r == '\\' || r == '"' || r == utf8.RuneError
	}) >= 0 {
		return strconv.Quote(path)
	}
	return path
}

func diffRawCaptureLimit(maxDiffBytes int) int {
	if maxDiffBytes > int(^uint(0)>>1)-diffRedactionLookahead {
		return maxDiffBytes
	}
	return maxDiffBytes + diffRedactionLookahead
}

type boundedDiffBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func newBoundedDiffBuffer(limit int) *boundedDiffBuffer {
	if limit < 0 {
		limit = 0
	}
	return &boundedDiffBuffer{limit: limit}
}

func (b *boundedDiffBuffer) Write(data []byte) (int, error) {
	original := len(data)
	remaining := b.limit - b.buf.Len()
	if remaining > 0 {
		if len(data) > remaining {
			data = data[:remaining]
		}
		_, _ = b.buf.Write(data)
	}
	if original > remaining {
		b.truncated = true
	}
	return original, nil
}

func (b *boundedDiffBuffer) String() string {
	return b.buf.String()
}

func (b *boundedDiffBuffer) Truncated() bool {
	return b.truncated
}

func (b *boundedDiffBuffer) MarkTruncated() {
	b.truncated = true
}

var _ io.Writer = (*boundedDiffBuffer)(nil)
