package embedding

import "testing"

func TestParseMode(t *testing.T) {
	cases := []struct {
		raw  string
		want Mode
	}{
		{"", DefaultMode},
		{"auto", ModeAuto},
		{" AUTO ", ModeAuto},
		{"true", ModeRequired},
		{"1", ModeRequired},
		{"T", ModeRequired},
		{"false", ModeOff},
		{"0", ModeOff},
	}
	for _, tc := range cases {
		got, err := ParseMode(tc.raw)
		if err != nil {
			t.Fatalf("ParseMode(%q) error = %v", tc.raw, err)
		}
		if got != tc.want {
			t.Fatalf("ParseMode(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestParseModeRejectsGarbage(t *testing.T) {
	for _, raw := range []string{"yes", "on", "semantic", "-1"} {
		if _, err := ParseMode(raw); err == nil {
			t.Fatalf("ParseMode(%q) succeeded, want error", raw)
		}
	}
}

func TestModeEnabled(t *testing.T) {
	if ModeOff.Enabled() {
		t.Fatal("ModeOff must not be enabled")
	}
	if !ModeAuto.Enabled() || !ModeRequired.Enabled() {
		t.Fatal("auto and required must be enabled")
	}
}

func TestDefaultModeIsAuto(t *testing.T) {
	if DefaultMode != ModeAuto {
		t.Fatalf("DefaultMode = %q, want auto", DefaultMode)
	}
	mode, err := ParseMode("")
	if err != nil {
		t.Fatal(err)
	}
	if mode != ModeAuto {
		t.Fatalf("unset mode = %q, want auto", mode)
	}
}
