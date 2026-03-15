package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

func TestToolsListIncludesMetadata(t *testing.T) {
	root, idx := setupContextTestRepo(t)
	s := &Server{
		root: root,
		idx:  idx,
		tools: []Tool{
			newRepoContextTool(root, idx),
		},
	}

	resp := s.handleRPC(context.Background(), rpcRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/list",
	})

	b, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatal(err)
	}

	var decoded struct {
		Tools []struct {
			Name         string         `json:"name"`
			Title        string         `json:"title"`
			Annotations  map[string]any `json:"annotations"`
			OutputSchema map[string]any `json:"outputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Tools) != 1 {
		t.Fatalf("expected one tool, got %d", len(decoded.Tools))
	}
	tool := decoded.Tools[0]

	if tool.Name != "repo_context" {
		t.Fatalf("expected tool name repo_context, got %q", tool.Name)
	}
	if tool.Title == "" {
		t.Fatal("expected title in tool metadata")
	}
	if got, ok := tool.Annotations["readOnlyHint"].(bool); !ok || !got {
		t.Fatalf("expected readOnlyHint=true, got %#v", tool.Annotations["readOnlyHint"])
	}
	if len(tool.OutputSchema) == 0 {
		t.Fatal("expected outputSchema in tool metadata")
	}
}

func TestCallToolReturnsStructuredContent(t *testing.T) {
	root, idx := setupContextTestRepo(t)
	s := &Server{
		root: root,
		idx:  idx,
		tools: []Tool{
			newRepoIndexStatusTool(idx),
		},
	}

	result, err := s.callTool(context.Background(), toolCallParams{
		Name:      "repo_index_status",
		Arguments: json.RawMessage([]byte(`{}`)),
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Content) != 1 || result.Content[0].Type != "text" || result.Content[0].Text == "" {
		t.Fatalf("expected text JSON fallback content, got %#v", result.Content)
	}
	if result.StructuredContent == nil {
		t.Fatal("expected structuredContent")
	}

	decoded, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structuredContent object, got %T", result.StructuredContent)
	}
	if _, ok := decoded["ready"]; !ok {
		t.Fatalf("expected index status fields in structuredContent, got %#v", decoded)
	}
}
