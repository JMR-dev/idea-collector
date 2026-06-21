package authcode

import (
	"regexp"
	"testing"
)

var formatRE = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{4}-[0-9A-HJKMNP-TV-Z]{4}-[0-9A-HJKMNP-TV-Z]{4}$`)

func TestNewFormat(t *testing.T) {
	for i := 0; i < 1000; i++ {
		c := New()
		if !formatRE.MatchString(c) {
			t.Fatalf("code %q does not match expected format", c)
		}
	}
}

func TestNewIsRandom(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		c := New()
		if seen[c] {
			t.Fatalf("duplicate code generated: %q", c)
		}
		seen[c] = true
	}
}

func TestNormalize(t *testing.T) {
	tests := []struct{ in, want string }{
		{"abcd-1234-wxyz", "ABCD1234WXYZ"},
		{" ABCD 1234 WXYZ ", "ABCD1234WXYZ"},
		{"oiln-uuuu", "011NVVVV"}, // O->0, I->1, L->1, N stays, U->V
		{"AB-CD", "ABCD"},
	}
	for _, tc := range tests {
		if got := Normalize(tc.in); got != tc.want {
			t.Errorf("Normalize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
