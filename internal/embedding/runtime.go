package embedding

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// DefaultRuntimeBackoff is how long an unreachable runtime is left alone
// before the next attempt is allowed through.
const DefaultRuntimeBackoff = 30 * time.Second

// probeSentinel is the input used to test a runtime end to end. A reachability
// check would not notice a runtime whose model was never pulled.
const probeSentinel = "memento embedding runtime probe"

// ErrRuntimeUnavailable is returned when the runtime is disabled or inside its
// backoff window. Callers degrade to lexical retrieval.
var ErrRuntimeUnavailable = errors.New("embedding runtime unavailable")

// Availability describes whether embedding can be attempted right now. It is
// deliberately separate from the embedder's identity: a runtime being down
// must never change what the stored vectors were made with.
type Availability struct {
	Available bool      `json:"available"`
	Reason    string    `json:"reason,omitempty"`
	CheckedAt time.Time `json:"checkedAt,omitempty"`
}

// Runtime wraps a concrete Embedder with availability tracking. It implements
// Embedder, so consumers take it wherever an embedder is accepted.
type Runtime struct {
	mu       sync.Mutex
	embedder Embedder
	mode     Mode
	backoff  time.Duration
	now      func() time.Time

	availability Availability
	probed       bool
	retryAt      time.Time
	attempting   bool
}

// NewRuntime wraps embedder. A nil embedder yields a runtime that is always
// unavailable, which is the correct shape for ModeOff.
func NewRuntime(embedder Embedder, mode Mode) *Runtime {
	return &Runtime{
		embedder: embedder,
		mode:     mode,
		backoff:  DefaultRuntimeBackoff,
		now:      time.Now,
	}
}

func (r *Runtime) Mode() Mode { return r.mode }

// Fingerprint delegates to the wrapped embedder and never reflects
// availability.
func (r *Runtime) Fingerprint() string {
	if r.embedder == nil {
		return ""
	}
	return r.embedder.Fingerprint()
}

func (r *Runtime) Name() string {
	if r.embedder == nil {
		return ""
	}
	return r.embedder.Name()
}

// Availability returns the last known state without performing any I/O.
func (r *Runtime) Availability() Availability {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.availability
}

// Probe runs one sentinel embed and records the outcome. Doctor and status use
// it to get a fresh answer; indexing does not need it, because a real Embed
// already doubles as a probe.
func (r *Runtime) Probe(ctx context.Context) Availability {
	_, _ = r.embed(ctx, TaskQuery, []string{probeSentinel})
	return r.Availability()
}

// Embed forwards to the wrapped embedder when the runtime is usable. Failures
// mark the runtime unavailable and open a backoff window, so a down runtime is
// attempted at most once per window.
func (r *Runtime) Embed(ctx context.Context, task Task, inputs []string) ([][]float32, error) {
	return r.embed(ctx, task, inputs)
}

func (r *Runtime) embed(ctx context.Context, task Task, inputs []string) ([][]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !r.mode.Enabled() || r.embedder == nil {
		return nil, ErrRuntimeUnavailable
	}
	r.mu.Lock()
	if r.attempting || (r.probed && !r.availability.Available && r.now().Before(r.retryAt)) {
		r.mu.Unlock()
		return nil, ErrRuntimeUnavailable
	}
	r.attempting = true
	r.mu.Unlock()

	vectors, err := r.embedder.Embed(ctx, task, inputs)
	if contextErr := ctx.Err(); contextErr != nil {
		// A context cancelled during dispatch says nothing about the runtime.
		r.finishAttempt()
		return nil, contextErr
	}
	if err != nil {
		r.markUnavailable(err)
		return nil, err
	}
	r.markAvailable()
	return vectors, nil
}

func (r *Runtime) markAvailable() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.attempting = false
	r.probed = true
	r.retryAt = time.Time{}
	r.availability = Availability{Available: true, CheckedAt: r.now()}
}

func (r *Runtime) markUnavailable(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.attempting = false
	r.probed = true
	r.retryAt = r.now().Add(r.backoff)
	r.availability = Availability{
		Available: false,
		Reason:    classifyReason(r.name(), err),
		CheckedAt: r.now(),
	}
}

func (r *Runtime) finishAttempt() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.attempting = false
}

func (r *Runtime) name() string {
	if r.embedder == nil {
		return ""
	}
	return r.embedder.Name()
}

// expireBackoffForTest lets tests simulate the window elapsing.
func (r *Runtime) expireBackoffForTest() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.retryAt = time.Time{}
}

// classifyReason turns a transport or protocol error into text a user can act
// on. Reason quality is what makes the status hint useful.
func classifyReason(name string, err error) string {
	if err == nil {
		return ""
	}
	message := strings.ToLower(err.Error())

	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) || strings.Contains(message, "timeout") {
		return "embedding runtime did not respond in time"
	}
	if strings.Contains(message, "connection refused") || strings.Contains(message, "no such host") || strings.Contains(message, "connect:") {
		return "no embedding runtime detected"
	}
	if strings.Contains(message, "404") || strings.Contains(message, "not found") {
		if name != "" {
			return fmt.Sprintf("model %s is not available in the embedding runtime", name)
		}
		return "the configured model is not available in the embedding runtime"
	}
	return err.Error()
}
