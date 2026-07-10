package redact

import (
	"strings"
	"testing"
)

func TestRedactKnownSecrets(t *testing.T) {
	r := Default()
	fixtures := []string{
		`API_KEY="super-secret-value-123456"`,
		`password = "secret value with spaces"`,
		`DATABASE_PASSWORD="correct horse battery staple"`,
		`OPENAI_API_KEY="short-value"`,
		`TOKEN=abc.def`,
		`"client_secret": "client-secret-value-123456"`,
		`Authorization: Bearer bearer.token.value-123456789`,
		`postgres://admin:database-password@example.test/app`,
		`github_pat_11AA22BB33CC44DD55EE66FF77`,
		`sk-proj-AbCdEf0123456789AbCdEf012345`,
	}
	for _, fixture := range fixtures {
		got := r.Redact(fixture)
		if !strings.Contains(got, Marker) || got == fixture {
			t.Errorf("expected secret fixture to be redacted: %q -> %q", fixture, got)
		}
	}
}

func TestRedactTruncatedQuotedAssignment(t *testing.T) {
	input := `DATABASE_PASSWORD="` + strings.Repeat("a", 1000)
	got := Default().Redact(input)
	if strings.Contains(got, strings.Repeat("a", 20)) || !strings.Contains(got, Marker) {
		t.Fatalf("expected unterminated quoted value to be redacted: %q", got)
	}
}

func TestRedactPrivateKeyPreservesLineCount(t *testing.T) {
	input := "before\n-----BEGIN PRIVATE KEY-----\nabc123\ndef456\n-----END PRIVATE KEY-----\nafter\n"
	got := Default().Redact(input)
	if strings.Count(got, "\n") != strings.Count(input, "\n") {
		t.Fatalf("expected line count to be preserved: %q", got)
	}
	if strings.Contains(got, "abc123") || !strings.Contains(got, Marker) {
		t.Fatalf("expected private key body to be redacted: %q", got)
	}
}

func TestRedactEntropyAndConfiguration(t *testing.T) {
	r, err := New(Config{
		AdditionalPatterns: []string{`INTERNAL-[A-Z0-9]{12}`},
		AllowPatterns:      []string{`ALLOW-[A-Za-z0-9+/]{30}`},
	})
	if err != nil {
		t.Fatal(err)
	}
	input := "INTERNAL-ABCDEF123456 random=A1b2C3d4E5f6G7h8I9j0K1l2M3n4 ALLOW-AbCdEfGhIjKlMnOpQrStUvWxYz1234"
	got := r.Redact(input)
	if strings.Count(got, Marker) != 2 {
		t.Fatalf("expected custom and entropy values redacted: %q", got)
	}
	if !strings.Contains(got, "ALLOW-AbCdEfGhIjKlMnOpQrStUvWxYz1234") {
		t.Fatalf("expected allowed fixture to remain: %q", got)
	}
}

func TestRedactHighEntropyHex(t *testing.T) {
	hexSecret := "0123456789abcdef0123456789abcdef"
	if got := Default().Redact(hexSecret); got != Marker {
		t.Fatalf("expected high-entropy hexadecimal value to be redacted, got %q", got)
	}
}

func TestRedactDisabled(t *testing.T) {
	r, err := New(Config{Disabled: true})
	if err != nil {
		t.Fatal(err)
	}
	input := `password="do-not-redact"`
	if got := r.Redact(input); got != input {
		t.Fatalf("expected opt-out to preserve input, got %q", got)
	}
}

func TestRedactDoesNotExemptCredentialNamedCodeExpressions(t *testing.T) {
	input := "token := jwt.New()\npassword = config.Password\n"
	if got := Default().Redact(input); strings.Count(got, Marker) != 2 {
		t.Fatalf("expected credential-named assignments to be redacted by default, got %q", got)
	}
}

func TestNewRejectsInvalidPattern(t *testing.T) {
	if _, err := New(Config{AdditionalPatterns: []string{"["}}); err == nil {
		t.Fatal("expected invalid custom pattern to fail")
	}
	if _, err := New(Config{AllowPatterns: []string{".*"}}); err == nil {
		t.Fatal("expected an allow pattern matching empty input to fail")
	}
}

func TestFingerprintChangesWithConfiguration(t *testing.T) {
	a := Default().Fingerprint()
	b, err := New(Config{AllowPatterns: []string{"fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	if a == b.Fingerprint() {
		t.Fatal("expected redaction configuration to affect fingerprint")
	}
}

func TestFingerprintPreservesAdditionalPatternOrder(t *testing.T) {
	a, err := New(Config{AdditionalPatterns: []string{"abc", "bc"}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := New(Config{AdditionalPatterns: []string{"bc", "abc"}})
	if err != nil {
		t.Fatal(err)
	}
	if a.Fingerprint() == b.Fingerprint() {
		t.Fatal("expected additional-pattern order to affect fingerprint")
	}
}

func TestFromEnv(t *testing.T) {
	t.Setenv("MEMENTO_REDACTION_ADDITIONAL_PATTERNS", `["CUSTOM-[0-9]{8}"]`)
	t.Setenv("MEMENTO_REDACTION_ALLOW_PATTERNS", `["CUSTOM-00000000"]`)
	t.Setenv("MEMENTO_REDACTION_HEX_ENTROPY_THRESHOLD", "3.6")
	r, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	got := r.Redact("CUSTOM-12345678 CUSTOM-00000000")
	if got != Marker+" CUSTOM-00000000" {
		t.Fatalf("unexpected env-configured redaction: %q", got)
	}
}
