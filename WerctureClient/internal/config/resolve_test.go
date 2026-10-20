package config

import "testing"

func TestEffectiveModeFlagOverridesProfile(t *testing.T) {
	if got := EffectiveMode("compress", "webp"); got != "webp" {
		t.Fatalf("flag %s", got)
	}
	if got := EffectiveMode("compress", ""); got != "compress" {
		t.Fatalf("saved %s", got)
	}
	if got := EffectiveMode("", "none"); got != "none" {
		t.Fatalf("explicit none %s", got)
	}
	if got := EffectiveQuality(60, 90); got != 90 {
		t.Fatalf("quality flag %d", got)
	}
	if got := EffectiveQuality(60, 0); got != 60 {
		t.Fatalf("quality saved %d", got)
	}
	if got := EffectiveMode("none", "both"); got != "both" {
		t.Fatalf("both %s", got)
	}
}

func TestSwitches(t *testing.T) {
	cases := []struct {
		mode           string
		compress, webp bool
	}{
		{"", false, false},
		{"none", false, false},
		{"compress", true, false},
		{"webp", false, true},
		{"both", true, true},
	}
	for _, tc := range cases {
		compress, webp := Switches(tc.mode)
		if compress != tc.compress || webp != tc.webp || ModeFromSwitches(compress, webp) != normalizeMode(tc.mode) {
			t.Fatalf("%s -> %v %v", tc.mode, compress, webp)
		}
	}
}

func normalizeMode(mode string) string {
	if mode == "" {
		return "none"
	}
	return mode
}
