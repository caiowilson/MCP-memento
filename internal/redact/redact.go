package redact

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	Marker                     = "[REDACTED]"
	defaultEntropyThreshold    = 4.3
	defaultHexEntropyThreshold = 3.5
	defaultMinTokenLength      = 24
	algorithmVersion           = 1
)

type Config struct {
	Disabled            bool
	EntropyDisabled     bool
	EntropyThreshold    float64
	HexEntropyThreshold float64
	MinTokenLength      int
	AdditionalPatterns  []string
	AllowPatterns       []string
}

type Redactor struct {
	disabled            bool
	entropyDisabled     bool
	entropyThreshold    float64
	hexEntropyThreshold float64
	minTokenLength      int
	additional          []*regexp.Regexp
	allow               []*regexp.Regexp
	fingerprint         string
}

var (
	assignmentPattern    = regexp.MustCompile(`(?im)((?:^|[^A-Za-z0-9_])["']?[A-Za-z0-9_-]*?(?:api[_-]?key|access[_-]?key|secret(?:[_-]?key)?|token|password|passwd|client[_-]?secret|private[_-]?key)(?:[_-][A-Za-z0-9]+)*["']?\s*(?::=|=|:)\s*)((?:"(?:\\.|[^"\\\r\n])*(?:"|$)|'(?:\\.|[^'\\\r\n])*(?:'|$)|[^\s"';,}#]+))`)
	authorizationPattern = regexp.MustCompile(`(?im)(\bauthorization\s*:\s*(?:bearer|basic)\s+)([A-Za-z0-9._~+/=-]+)`)
	urlCredentialPattern = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://[^\s:/@]+:)([^\s/@]+)(@)`)
	privateKeyPattern    = regexp.MustCompile(`(?s)-----BEGIN (?:[A-Z0-9 ]+ )?PRIVATE KEY-----(?:.*?-----END (?:[A-Z0-9 ]+ )?PRIVATE KEY-----|.*$)`)
	knownTokenPattern    = regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|sk-(?:proj-)?[A-Za-z0-9_-]{20,}|AKIA[0-9A-Z]{16}|ASIA[0-9A-Z]{16}|AIza[0-9A-Za-z_-]{30,}|xox[baprs]-[A-Za-z0-9-]{10,}|sk_live_[A-Za-z0-9]{16,}|eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,})\b`)
	entropyTokenPattern  = regexp.MustCompile(`[A-Za-z0-9_+/=-]{24,}`)
	hexTokenPattern      = regexp.MustCompile(`\b[A-Fa-f0-9]{24,}\b`)
)

func Default() *Redactor {
	r, err := New(Config{})
	if err != nil {
		panic(err)
	}
	return r
}

func FromEnv() (*Redactor, error) {
	enabled, err := envBool("MEMENTO_REDACTION_ENABLED", true)
	if err != nil {
		return nil, err
	}
	entropyEnabled, err := envBool("MEMENTO_REDACTION_ENTROPY_ENABLED", true)
	if err != nil {
		return nil, err
	}
	threshold, err := envFloat("MEMENTO_REDACTION_ENTROPY_THRESHOLD", defaultEntropyThreshold)
	if err != nil {
		return nil, err
	}
	hexThreshold, err := envFloat("MEMENTO_REDACTION_HEX_ENTROPY_THRESHOLD", defaultHexEntropyThreshold)
	if err != nil {
		return nil, err
	}
	minLength, err := envInt("MEMENTO_REDACTION_MIN_TOKEN_LENGTH", defaultMinTokenLength)
	if err != nil {
		return nil, err
	}
	additional, err := envPatterns("MEMENTO_REDACTION_ADDITIONAL_PATTERNS")
	if err != nil {
		return nil, err
	}
	allow, err := envPatterns("MEMENTO_REDACTION_ALLOW_PATTERNS")
	if err != nil {
		return nil, err
	}
	return New(Config{
		Disabled:            !enabled,
		EntropyDisabled:     !entropyEnabled,
		EntropyThreshold:    threshold,
		HexEntropyThreshold: hexThreshold,
		MinTokenLength:      minLength,
		AdditionalPatterns:  additional,
		AllowPatterns:       allow,
	})
}

func New(cfg Config) (*Redactor, error) {
	if cfg.EntropyThreshold <= 0 {
		cfg.EntropyThreshold = defaultEntropyThreshold
	}
	if cfg.HexEntropyThreshold <= 0 {
		cfg.HexEntropyThreshold = defaultHexEntropyThreshold
	}
	if cfg.MinTokenLength <= 0 {
		cfg.MinTokenLength = defaultMinTokenLength
	}
	r := &Redactor{
		disabled:            cfg.Disabled,
		entropyDisabled:     cfg.EntropyDisabled,
		entropyThreshold:    cfg.EntropyThreshold,
		hexEntropyThreshold: cfg.HexEntropyThreshold,
		minTokenLength:      cfg.MinTokenLength,
	}
	var err error
	r.additional, err = compilePatterns("additional", cfg.AdditionalPatterns)
	if err != nil {
		return nil, err
	}
	r.allow, err = compilePatterns("allow", cfg.AllowPatterns)
	if err != nil {
		return nil, err
	}

	fingerprintConfig := struct {
		Version             int      `json:"version"`
		Disabled            bool     `json:"disabled"`
		EntropyDisabled     bool     `json:"entropyDisabled"`
		EntropyThreshold    float64  `json:"entropyThreshold"`
		HexEntropyThreshold float64  `json:"hexEntropyThreshold"`
		MinTokenLength      int      `json:"minTokenLength"`
		AdditionalPatterns  []string `json:"additionalPatterns"`
		AllowPatterns       []string `json:"allowPatterns"`
	}{algorithmVersion, cfg.Disabled, cfg.EntropyDisabled, cfg.EntropyThreshold, cfg.HexEntropyThreshold, cfg.MinTokenLength, append([]string(nil), cfg.AdditionalPatterns...), sortedCopy(cfg.AllowPatterns)}
	b, _ := json.Marshal(fingerprintConfig)
	sum := sha256.Sum256(b)
	r.fingerprint = hex.EncodeToString(sum[:16])
	return r, nil
}

func (r *Redactor) Fingerprint() string {
	if r == nil {
		return Default().Fingerprint()
	}
	return r.fingerprint
}

func (r *Redactor) Redact(input string) string {
	if r == nil {
		r = Default()
	}
	if r.disabled || input == "" {
		return input
	}

	out := replaceAssignments(input, r.allowed)
	out = replaceSecretGroup(out, authorizationPattern, 2, r.allowed)
	out = replaceSecretGroup(out, urlCredentialPattern, 2, r.allowed)
	out = replaceMatches(out, privateKeyPattern, r.allowed, preserveLines)
	out = replaceMatches(out, knownTokenPattern, r.allowed, func(string) string { return Marker })
	for _, pattern := range r.additional {
		out = replaceMatches(out, pattern, r.allowed, func(string) string { return Marker })
	}
	if !r.entropyDisabled {
		out = replaceMatches(out, hexTokenPattern, r.allowed, func(candidate string) string {
			if len(candidate) >= r.minTokenLength && containsHexLettersAndDigits(candidate) && shannonEntropy(candidate) >= r.hexEntropyThreshold {
				return Marker
			}
			return candidate
		})
		out = replaceMatches(out, entropyTokenPattern, r.allowed, func(candidate string) string {
			if len(candidate) >= r.minTokenLength && hasTokenDiversity(candidate) && shannonEntropy(candidate) >= r.entropyThreshold {
				return Marker
			}
			return candidate
		})
	}
	return out
}

func replaceAssignments(input string, allowed func(string) bool) string {
	return assignmentPattern.ReplaceAllStringFunc(input, func(match string) string {
		parts := assignmentPattern.FindStringSubmatchIndex(match)
		start, end := parts[4], parts[5]
		secret := match[start:end]
		if allowed(secret) {
			return match
		}
		return match[:start] + Marker + match[end:]
	})
}

func (r *Redactor) allowed(candidate string) bool {
	for _, pattern := range r.allow {
		if pattern.MatchString(candidate) {
			return true
		}
	}
	return false
}

func replaceSecretGroup(input string, pattern *regexp.Regexp, secretGroup int, allowed func(string) bool) string {
	return pattern.ReplaceAllStringFunc(input, func(match string) string {
		parts := pattern.FindStringSubmatchIndex(match)
		start, end := parts[secretGroup*2], parts[secretGroup*2+1]
		secret := match[start:end]
		if allowed(secret) {
			return match
		}
		return match[:start] + Marker + match[end:]
	})
}

func replaceMatches(input string, pattern *regexp.Regexp, allowed func(string) bool, replacement func(string) string) string {
	return pattern.ReplaceAllStringFunc(input, func(match string) string {
		if match == Marker || allowed(match) {
			return match
		}
		return replacement(match)
	})
}

func preserveLines(match string) string {
	return Marker + strings.Repeat("\n", strings.Count(match, "\n"))
}

func hasTokenDiversity(token string) bool {
	classes := 0
	checks := []string{"abcdefghijklmnopqrstuvwxyz", "ABCDEFGHIJKLMNOPQRSTUVWXYZ", "0123456789", "_+/=-"}
	for _, chars := range checks {
		if strings.ContainsAny(token, chars) {
			classes++
		}
	}
	return classes >= 3
}

func containsHexLettersAndDigits(token string) bool {
	return strings.ContainsAny(token, "abcdefABCDEF") && strings.ContainsAny(token, "0123456789")
}

func shannonEntropy(value string) float64 {
	counts := map[rune]int{}
	for _, ch := range value {
		counts[ch]++
	}
	length := float64(len([]rune(value)))
	if length == 0 {
		return 0
	}
	var entropy float64
	for _, count := range counts {
		p := float64(count) / length
		entropy -= p * math.Log2(p)
	}
	return entropy
}

func compilePatterns(kind string, patterns []string) ([]*regexp.Regexp, error) {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, raw := range patterns {
		pattern, err := regexp.Compile(raw)
		if err != nil {
			return nil, fmt.Errorf("compile %s redaction pattern %q: %w", kind, raw, err)
		}
		if pattern.MatchString("") {
			return nil, fmt.Errorf("compile %s redaction pattern %q: pattern must not match an empty string", kind, raw)
		}
		out = append(out, pattern)
	}
	return out, nil
}

func envBool(name string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("parse %s: %w", name, err)
	}
	return value, nil
}

func envFloat(name string, fallback float64) (float64, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("parse %s: expected a positive number", name)
	}
	return value, nil
}

func envInt(name string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("parse %s: expected a positive integer", name)
	}
	return value, nil
}

func envPatterns(name string) ([]string, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return nil, nil
	}
	var patterns []string
	if err := json.Unmarshal([]byte(raw), &patterns); err != nil {
		return nil, fmt.Errorf("parse %s as a JSON string array: %w", name, err)
	}
	return patterns, nil
}

func sortedCopy(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}
