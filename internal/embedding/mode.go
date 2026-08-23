package embedding

import (
	"fmt"
	"strconv"
	"strings"
)

// Mode selects how semantic retrieval behaves.
type Mode string

const (
	// ModeOff disables semantic retrieval. No embedder is constructed.
	ModeOff Mode = "off"
	// ModeAuto uses semantic retrieval when a runtime is reachable and falls
	// back to lexical otherwise. Falling back is a healthy state.
	ModeAuto Mode = "auto"
	// ModeRequired behaves like ModeAuto but reports an unreachable runtime as
	// an error. It still serves lexical results.
	ModeRequired Mode = "required"
)

// DefaultMode is the mode used when MEMENTO_SEMANTIC_ENABLED is unset.
var DefaultMode = ModeOff

// Enabled reports whether an embedder should be constructed.
func (m Mode) Enabled() bool { return m == ModeAuto || m == ModeRequired }

// ParseMode reads the tri-state MEMENTO_SEMANTIC_ENABLED value. The legacy
// boolean spellings keep working; "true" now means "required".
func ParseMode(raw string) (Mode, error) {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	if trimmed == "" {
		return DefaultMode, nil
	}
	if trimmed == string(ModeAuto) {
		return ModeAuto, nil
	}
	value, err := strconv.ParseBool(trimmed)
	if err != nil {
		return "", fmt.Errorf("parse MEMENTO_SEMANTIC_ENABLED: %q is not auto, true, or false", raw)
	}
	if value {
		return ModeRequired, nil
	}
	return ModeOff, nil
}
