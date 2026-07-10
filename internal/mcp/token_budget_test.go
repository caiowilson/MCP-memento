package mcp

import (
	"context"
	"strings"
	"testing"
)

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		content string
		want    int
	}{
		{content: "", want: 0},
		{content: "a", want: 1},
		{content: "abcd", want: 1},
		{content: "abcde", want: 2},
		{content: "🙂", want: 1},
	}
	for _, tc := range tests {
		if got := estimateTokens(tc.content); got != tc.want {
			t.Errorf("estimateTokens(%q)=%d, want %d", tc.content, got, tc.want)
		}
	}
}

func TestContextBudgetUsesTokensAsPrimaryAndBytesAsHardCeiling(t *testing.T) {
	tokenLimited := newContextBudget(2, 100)
	if !tokenLimited.tryAdd("12345678") {
		t.Fatal("expected content at the token limit to fit")
	}
	if tokenLimited.tryAdd("x") || !tokenLimited.clamped {
		t.Fatal("expected token budget to reject additional content")
	}

	byteLimited := newContextBudget(100, 4)
	if byteLimited.tryAdd("12345") || !byteLimited.clamped {
		t.Fatal("expected byte ceiling to remain enforced")
	}
}

func TestContextBudgetContinuesAfterOversizedCandidate(t *testing.T) {
	budget := newContextBudget(4, 100)
	if budget.tryAdd(strings.Repeat("x", 20)) {
		t.Fatal("expected oversized candidate to be rejected")
	}
	if !budget.tryAdd("small") {
		t.Fatal("expected a later smaller candidate to fit")
	}
	if budget.usedTokens != 2 || budget.usedBytes != 5 || !budget.clamped {
		t.Fatalf("unexpected budget state: %#v", budget)
	}
}

func TestCompareSignalDensity(t *testing.T) {
	if got := compareSignalDensity(10, 100, 4, 10); got >= 0 {
		t.Fatalf("expected smaller high-density candidate to rank first, got %d", got)
	}
	if got := compareSignalDensity(4, 10, 4, 20); got <= 0 {
		t.Fatalf("expected lower token cost to break equal-signal tie, got %d", got)
	}
	if got := compareSignalDensity(4, 10, 4, 10); got != 0 {
		t.Fatalf("expected identical candidates to compare equal, got %d", got)
	}
}

func TestRepoContextTokenBudgetAppliesToEveryMode(t *testing.T) {
	root, idx := setupContextTestRepo(t)
	tool := newRepoContextTool(root, idx)

	for _, mode := range []string{"outline", "summary", "auto", "full"} {
		t.Run(mode, func(t *testing.T) {
			got, err := tool.Handler(context.Background(), rawJSON(t, map[string]any{
				"path":          "pkg/a.go",
				"mode":          mode,
				"maxTokens":     2,
				"maxTotalBytes": 1_000_000,
			}))
			if err != nil {
				t.Fatal(err)
			}
			limits := got.(map[string]any)["limits"].(map[string]any)
			if limits["maxTokens"] != 2 {
				t.Fatalf("expected maxTokens=2, got %#v", limits["maxTokens"])
			}
			if used, _ := limits["usedTokens"].(int); used > 2 {
				t.Fatalf("token budget exceeded: %#v", limits)
			}
			if estimator := limits["tokenEstimator"]; estimator != contextTokenEstimator {
				t.Fatalf("unexpected estimator: %#v", estimator)
			}
			if clamped, _ := limits["clamped"].(bool); !clamped {
				t.Fatalf("expected small token budget to clamp %s mode: %#v", mode, limits)
			}
		})
	}
}

func TestRepoContextTokenBudgetEnvironmentAndArgumentOverride(t *testing.T) {
	t.Setenv("MEMENTO_CONTEXT_MAX_TOKENS", "3")
	root, idx := setupContextTestRepo(t)
	tool := newRepoContextTool(root, idx)

	fromEnv, err := tool.Handler(context.Background(), rawJSON(t, map[string]any{"path": "pkg/a.go"}))
	if err != nil {
		t.Fatal(err)
	}
	envLimits := fromEnv.(map[string]any)["limits"].(map[string]any)
	if envLimits["maxTokens"] != 3 {
		t.Fatalf("expected environment default maxTokens=3, got %#v", envLimits)
	}

	overridden, err := tool.Handler(context.Background(), rawJSON(t, map[string]any{
		"path":      "pkg/a.go",
		"maxTokens": 100,
	}))
	if err != nil {
		t.Fatal(err)
	}
	overrideLimits := overridden.(map[string]any)["limits"].(map[string]any)
	if overrideLimits["maxTokens"] != 100 {
		t.Fatalf("expected argument override maxTokens=100, got %#v", overrideLimits)
	}
	if len(contextResultFiles(t, overridden)) == 0 {
		t.Fatal("expected argument override to admit context files")
	}
}

var benchmarkPackedItems int

func BenchmarkContextPacking(b *testing.B) {
	contents := []string{
		strings.Repeat("a", 40_000),
		strings.Repeat("b", 4_096),
		strings.Repeat("c", 2_048),
		strings.Repeat("d", 1_024),
		strings.Repeat("e", 512),
	}

	b.Run("byte_only_baseline", func(b *testing.B) {
		for range b.N {
			used, packed := 0, 0
			for _, content := range contents {
				if used+len(content) > defaultRepoContextMaxTotalBytes {
					break
				}
				used += len(content)
				packed++
			}
			benchmarkPackedItems = packed
		}
	})

	b.Run("token_primary_with_byte_ceiling", func(b *testing.B) {
		for range b.N {
			budget := newContextBudget(defaultRepoContextMaxTokens, defaultRepoContextMaxTotalBytes)
			packed := 0
			for _, content := range contents {
				if budget.tryAdd(content) {
					packed++
				}
			}
			benchmarkPackedItems = packed
		}
	})
}
