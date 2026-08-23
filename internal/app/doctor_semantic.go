package app

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"memento-mcp/internal/embedding"
)

// doctorSemantic reports how semantic retrieval is configured and whether the
// runtime answers. It returns the number of failures to add to doctor's total:
// an unreachable runtime only fails in required mode, because falling back to
// lexical is a healthy state in auto.
func doctorSemantic(ctx context.Context, stdout io.Writer) int {
	config, err := embedding.FromEnv()
	if err != nil {
		fmt.Fprintf(stdout, "[FAIL] semantic: %v\n", err)
		return 1
	}
	if !config.Mode.Enabled() {
		fmt.Fprintln(stdout, "[PASS] semantic: disabled (MEMENTO_SEMANTIC_ENABLED=false)")
		return 0
	}

	runtime, ok := config.Embedder.(*embedding.Runtime)
	if !ok {
		fmt.Fprintf(stdout, "[FAIL] semantic: embedder is %T, expected a runtime\n", config.Embedder)
		return 1
	}

	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	availability := runtime.Probe(probeCtx)
	if availability.Available {
		fmt.Fprintf(stdout, "[PASS] semantic: %s reachable (mode %s)\n", runtime.Name(), config.Mode)
		return 0
	}

	label := "WARN"
	failures := 0
	if config.Mode == embedding.ModeRequired {
		label = "FAIL"
		failures = 1
	}
	fmt.Fprintf(stdout, "[%s] semantic: %s (mode %s)\n", label, availability.Reason, config.Mode)
	_, model, _ := strings.Cut(runtime.Name(), "/")
	fmt.Fprintf(stdout, "       install a local runtime, then run: ollama pull %s\n", model)
	if config.Mode == embedding.ModeAuto {
		fmt.Fprintln(stdout, "       retrieval continues to work using lexical ranking")
	}
	return failures
}
