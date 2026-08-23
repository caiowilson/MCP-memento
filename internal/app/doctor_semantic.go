package app

import (
	"context"
	"errors"
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
	return doctorSemanticProbe(ctx, stdout, config.Mode, runtime.Name(), runtime.Probe)
}

func doctorSemanticProbe(ctx context.Context, stdout io.Writer, mode embedding.Mode, name string, probe func(context.Context) embedding.Availability) int {
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	availability := probe(probeCtx)
	if availability.Available {
		fmt.Fprintf(stdout, "[PASS] semantic: %s reachable (mode %s)\n", name, mode)
		return 0
	}
	if err := probeCtx.Err(); err != nil {
		label := "WARN"
		failures := 0
		if mode == embedding.ModeRequired {
			label = "FAIL"
			failures = 1
		}
		if errors.Is(err, context.DeadlineExceeded) {
			fmt.Fprintf(stdout, "[%s] semantic: runtime probe timed out before it responded (mode %s)\n", label, mode)
			fmt.Fprintln(stdout, "       verify the local runtime is running and responsive, then rerun: memento-mcp doctor")
		} else {
			fmt.Fprintf(stdout, "[%s] semantic: runtime probe was canceled before it completed (mode %s)\n", label, mode)
			fmt.Fprintln(stdout, "       rerun doctor when the local runtime is available")
		}
		return failures
	}

	label := "WARN"
	failures := 0
	if mode == embedding.ModeRequired {
		label = "FAIL"
		failures = 1
	}
	fmt.Fprintf(stdout, "[%s] semantic: %s (mode %s)\n", label, availability.Reason, mode)
	_, model, _ := strings.Cut(name, "/")
	fmt.Fprintf(stdout, "       install a local runtime, then run: ollama pull %s\n", model)
	if mode == embedding.ModeAuto {
		fmt.Fprintln(stdout, "       retrieval continues to work using lexical ranking")
	}
	return failures
}
