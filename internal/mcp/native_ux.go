package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"memento-mcp/internal/parsing"
	"memento-mcp/internal/redact"
)

const (
	defaultResourcePageSize = 100
	defaultResourceMaxBytes = 32_000
	defaultPrimeMaxBytes    = 24_000
	defaultPrimeMaxNotes    = 8
)

type nativeRPCError struct {
	Code    int
	Message string
	Data    any
}

func (e *nativeRPCError) Error() string { return e.Message }

type resourceDescriptor struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

type resourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
}

func (s *Server) listResources(ctx context.Context, raw json.RawMessage) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root := s.currentRoot()
	store, err := NewNoteStore(root)
	if err != nil {
		return nil, err
	}
	notes, err := store.List()
	if err != nil {
		return nil, err
	}
	resources := make([]resourceDescriptor, 0, len(notes)+8)
	for _, note := range notes {
		if noteStatus(note) == NoteStatusTombstoned {
			continue
		}
		if len(note.Key) > 1_024 {
			continue
		}
		resources = append(resources, resourceDescriptor{
			URI:         noteResourceURI(note.Key),
			Name:        note.Key,
			Description: noteResourceDescription(note),
			MimeType:    "text/markdown",
		})
	}
	for _, path := range keyResourceFiles(root) {
		resources = append(resources, resourceDescriptor{
			URI:         fileResourceURI(path),
			Name:        path,
			Description: "Repository file: " + path,
			MimeType:    resourceMIMEType(path),
		})
	}
	offset, err := parseNativeCursor(raw, len(resources))
	if err != nil {
		return nil, err
	}
	end := min(offset+defaultResourcePageSize, len(resources))
	result := map[string]any{"resources": resources[offset:end]}
	if end < len(resources) {
		result["nextCursor"] = strconv.Itoa(end)
	}
	return result, nil
}

func listResourceTemplates() map[string]any {
	return map[string]any{"resourceTemplates": []map[string]any{{
		"uriTemplate": "repo://file/{path}",
		"name":        "Repository file",
		"description": "Read a redacted, bounded text file by repo-relative path.",
		"mimeType":    "text/plain",
	}}}
}

func (s *Server) readResource(ctx context.Context, raw json.RawMessage) (any, error) {
	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(raw, &params); err != nil || strings.TrimSpace(params.URI) == "" {
		return nil, &nativeRPCError{Code: -32602, Message: "Invalid params", Data: "uri is required"}
	}
	parsed, err := url.Parse(params.URI)
	if err != nil {
		return nil, resourceNotFound(params.URI)
	}
	identifier, err := url.PathUnescape(strings.TrimPrefix(parsed.EscapedPath(), "/"))
	if err != nil || identifier == "" {
		return nil, resourceNotFound(params.URI)
	}
	root := s.currentRoot()
	switch {
	case parsed.Scheme == "note" && parsed.Host == "memory":
		store, storeErr := NewNoteStore(root)
		if storeErr != nil {
			return nil, storeErr
		}
		note, readErr := store.Read(identifier)
		if readErr != nil {
			return nil, resourceNotFound(params.URI)
		}
		return map[string]any{"contents": []resourceContent{{URI: params.URI, MimeType: "text/markdown", Text: boundResourceText(renderNoteResource(note))}}}, nil
	case parsed.Scheme == "repo" && parsed.Host == "file":
		text, mimeType, readErr := readFileResource(ctx, root, identifier, s.redactor)
		if readErr != nil {
			return nil, resourceNotFound(params.URI)
		}
		return map[string]any{"contents": []resourceContent{{URI: params.URI, MimeType: mimeType, Text: text}}}, nil
	default:
		return nil, resourceNotFound(params.URI)
	}
}

func resourceNotFound(uri string) *nativeRPCError {
	return &nativeRPCError{Code: -32002, Message: "Resource not found", Data: map[string]any{"uri": uri}}
}

func noteResourceURI(key string) string {
	return (&url.URL{Scheme: "note", Host: "memory", Path: "/" + key}).String()
}

func fileResourceURI(path string) string {
	return (&url.URL{Scheme: "repo", Host: "file", Path: "/" + filepath.ToSlash(path)}).String()
}

func noteResourceDescription(note Note) string {
	description := "Durable repository note"
	if noteStatus(note) == NoteStatusStale {
		description += " (STALE: " + note.StaleReason + ")"
	}
	if note.Path != "" {
		description += " for " + note.Path
	}
	description, _ = truncateStringBytes(description, 240)
	return description
}

func renderNoteResource(note Note) string {
	var out strings.Builder
	fmt.Fprintf(&out, "# Memory: %s\n\n", note.Key)
	fmt.Fprintf(&out, "- Status: %s\n", noteStatus(note))
	fmt.Fprintf(&out, "- Updated: %s\n", note.UpdatedAt)
	if note.Path != "" {
		fmt.Fprintf(&out, "- Path: `%s`\n", note.Path)
	}
	if len(note.Tags) > 0 {
		fmt.Fprintf(&out, "- Tags: %s\n", strings.Join(note.Tags, ", "))
	}
	if note.StaleReason != "" {
		fmt.Fprintf(&out, "- Stale reason: %s\n", note.StaleReason)
	}
	out.WriteString("\n---\n\n")
	out.WriteString(note.Text)
	return out.String()
}

func keyResourceFiles(root string) []string {
	candidates := []string{
		"AGENTS.md", "README.md", "README.pt-BR.md", "go.mod", "package.json",
		"pyproject.toml", "Cargo.toml", "composer.json", "Makefile", "Taskfile.yml", "Taskfile.yaml",
	}
	files := make([]string, 0, len(candidates))
	ignored := loadGitIgnored(root)
	for _, candidate := range candidates {
		if ignored.Matches(candidate) {
			continue
		}
		abs, err := resolvedResourcePath(root, candidate)
		if err != nil {
			continue
		}
		info, err := os.Stat(abs)
		if err == nil && info.Mode().IsRegular() {
			files = append(files, candidate)
		}
	}
	return files
}

func readFileResource(ctx context.Context, root, rel string, redactor *redact.Redactor) (string, string, error) {
	rel = filepath.ToSlash(filepath.Clean(strings.TrimSpace(rel)))
	if !resourceFileAllowed(rel) {
		return "", "", fmt.Errorf("resource file is not allowed")
	}
	if loadGitIgnored(root).Matches(rel) {
		return "", "", fmt.Errorf("resource file is ignored by Git")
	}
	abs, err := resolvedResourcePath(root, rel)
	if err != nil {
		return "", "", err
	}
	info, err := os.Stat(abs)
	if err != nil || !info.Mode().IsRegular() {
		return "", "", fmt.Errorf("resource is not a regular file")
	}
	file, err := os.Open(abs)
	if err != nil {
		return "", "", err
	}
	defer file.Close()
	maxBytes := envInt("MEMENTO_RESOURCE_MAX_BYTES", defaultResourceMaxBytes)
	if maxBytes <= 0 {
		maxBytes = defaultResourceMaxBytes
	}
	content, err := readFileContent(ctx, file, 0, 0, maxBytes)
	if err != nil {
		return "", "", err
	}
	if strings.IndexByte(content, 0) >= 0 {
		return "", "", fmt.Errorf("resource is not UTF-8 text")
	}
	content, ok := validUTF8ResourcePrefix(content)
	if !ok {
		return "", "", fmt.Errorf("resource is not UTF-8 text")
	}
	return redactor.Redact(content), resourceMIMEType(rel), nil
}

func resolvedResourcePath(root, rel string) (string, error) {
	abs, err := safeJoin(root, rel)
	if err != nil {
		return "", err
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	within, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("resource resolves outside workspace")
	}
	return resolved, nil
}

func boundResourceText(content string) string {
	maxBytes := envInt("MEMENTO_RESOURCE_MAX_BYTES", defaultResourceMaxBytes)
	if maxBytes <= 0 {
		maxBytes = defaultResourceMaxBytes
	}
	if len(content) <= maxBytes {
		return content
	}
	marker := "\n\n[Resource truncated; use memento tools for deeper retrieval.]\n"
	if maxBytes <= len(marker) {
		return prefixStringBytes(content, maxBytes)
	}
	return prefixStringBytes(content, maxBytes-len(marker)) + marker
}

func validUTF8ResourcePrefix(content string) (string, bool) {
	for offset := 0; offset < len(content); {
		remaining := content[offset:]
		if !utf8.FullRuneInString(remaining) {
			return content[:offset], true
		}
		_, size := utf8.DecodeRuneInString(remaining)
		if size == 1 && remaining[0] >= utf8.RuneSelf {
			return "", false
		}
		offset += size
	}
	return content, true
}

func resourceFileAllowed(rel string) bool {
	if rel == "" || rel == "." || strings.HasPrefix(rel, "../") || filepath.IsAbs(rel) {
		return false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for _, part := range parts[:len(parts)-1] {
		if shouldIgnoreDir(part) {
			return false
		}
	}
	name := parts[len(parts)-1]
	if shouldIgnoreFile(name) {
		return false
	}
	lower := strings.ToLower(name)
	denied := []string{".key", ".pem", ".p12", ".pfx", ".crt", ".der", ".ppk", ".sqlite", ".db", ".bin", ".exe"}
	for _, suffix := range denied {
		if strings.HasSuffix(lower, suffix) {
			return false
		}
	}
	return lower != "id_rsa" && lower != "id_ed25519"
}

func resourceMIMEType(path string) string {
	if parsing.IsPHPPath(path) {
		return "application/x-httpd-php"
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md":
		return "text/markdown"
	case ".json":
		return "application/json"
	case ".yaml", ".yml":
		return "application/yaml"
	case ".go":
		return "text/x-go"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "text/javascript"
	case ".ts", ".tsx", ".mts", ".cts":
		return "text/typescript"
	default:
		return "text/plain"
	}
}

func parseNativeCursor(raw json.RawMessage, total int) (int, error) {
	if len(raw) == 0 {
		return 0, nil
	}
	var params struct {
		Cursor string `json:"cursor"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return 0, &nativeRPCError{Code: -32602, Message: "Invalid params", Data: err.Error()}
	}
	if params.Cursor == "" {
		return 0, nil
	}
	offset, err := strconv.Atoi(params.Cursor)
	if err != nil || offset < 0 || offset > total {
		return 0, &nativeRPCError{Code: -32602, Message: "Invalid params", Data: "invalid cursor"}
	}
	return offset, nil
}

func listPrompts() map[string]any {
	return map[string]any{"prompts": []map[string]any{{
		"name":        "prime",
		"description": "Prime the session with durable notes, project manifests, and optional active-file structure.",
		"arguments": []map[string]any{
			{"name": "path", "description": "Optional repo-relative active file to outline.", "required": false},
			{"name": "focus", "description": "Optional task, symbol, or question to prioritize.", "required": false},
		},
	}}}
}

func (s *Server) getPrompt(ctx context.Context, raw json.RawMessage) (any, error) {
	var params struct {
		Name      string            `json:"name"`
		Arguments map[string]string `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &params); err != nil || params.Name != "prime" {
		return nil, &nativeRPCError{Code: -32602, Message: "Invalid params", Data: "unknown prompt"}
	}
	text, err := buildPrimePrompt(ctx, s.currentRoot(), params.Arguments, s.redactor)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"description": "Prime memento repository context",
		"messages": []map[string]any{{
			"role":    "user",
			"content": map[string]any{"type": "text", "text": text},
		}},
	}, nil
}

func buildPrimePrompt(ctx context.Context, root string, args map[string]string, redactor *redact.Redactor) (string, error) {
	if len(args["path"]) > 4_096 || len(args["focus"]) > 2_000 {
		return "", &nativeRPCError{Code: -32602, Message: "Invalid params", Data: "prime arguments are too long"}
	}
	store, err := NewNoteStore(root)
	if err != nil {
		return "", err
	}
	notes, err := store.List()
	if err != nil {
		return "", err
	}
	active := make([]Note, 0, len(notes))
	for _, note := range notes {
		if noteStatus(note) != NoteStatusTombstoned {
			active = append(active, note)
		}
	}
	sort.SliceStable(active, func(i, j int) bool {
		if noteStatus(active[i]) != noteStatus(active[j]) {
			return noteStatus(active[i]) == NoteStatusFresh
		}
		return active[i].UpdatedAt > active[j].UpdatedAt
	})

	var out strings.Builder
	fmt.Fprintf(&out, "Prime the repository `%s` before beginning work. Treat stale memory as historical evidence that must be verified.\n", filepath.Base(root))
	if focus := strings.TrimSpace(args["focus"]); focus != "" {
		fmt.Fprintf(&out, "\nTask focus: %s\n", focus)
	}
	if path := strings.TrimSpace(args["path"]); path != "" {
		outlineArgs, _ := json.Marshal(map[string]any{"path": path, "maxSymbols": 80})
		outlineAny, outlineErr := newRepoOutlineTool(root, redactor).Handler(ctx, outlineArgs)
		if outlineErr != nil {
			return "", &nativeRPCError{Code: -32602, Message: "Invalid params", Data: outlineErr.Error()}
		}
		encoded, _ := json.MarshalIndent(outlineAny, "", "  ")
		fmt.Fprintf(&out, "\n## Active file outline\n\n```json\n%s\n```\n", encoded)
	}
	if len(active) > 0 {
		out.WriteString("\n## Durable memory\n")
		for index := 0; index < min(len(active), defaultPrimeMaxNotes); index++ {
			note, readErr := store.Read(active[index].Key)
			if readErr != nil {
				continue
			}
			text, _ := truncateStringBytes(note.Text, 1_200)
			fmt.Fprintf(&out, "\n### %s [%s]\n%s\n", note.Key, noteStatus(note), text)
			if note.StaleReason != "" {
				fmt.Fprintf(&out, "\nStale reason: %s\n", note.StaleReason)
			}
		}
	}
	out.WriteString("\n## Project files\n")
	for _, path := range keyResourceFiles(root) {
		content, _, readErr := readFileResource(ctx, root, path, redactor)
		if readErr != nil {
			continue
		}
		content, _ = truncateStringBytes(content, 4_000)
		fmt.Fprintf(&out, "\n### %s\n\n```text\n%s\n```\n", path, content)
	}
	out.WriteString("\nUse `repo_context` for task-specific retrieval, `repo_outline` for structure, and `memory_verify` or `memory_tombstone` after adjudicating stale notes. Do not assume an unverified stale note is current fact.\n")
	maxBytes := envInt("MEMENTO_PRIME_MAX_BYTES", defaultPrimeMaxBytes)
	if maxBytes < 1_000 {
		maxBytes = defaultPrimeMaxBytes
	}
	if out.Len() > maxBytes {
		marker := "\n\n[Prime context truncated; use memento tools for deeper retrieval.]\n"
		return prefixStringBytes(out.String(), maxBytes-len(marker)) + marker, nil
	}
	return out.String(), nil
}
