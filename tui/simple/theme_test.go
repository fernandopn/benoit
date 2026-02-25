package simple

import (
	"strings"
	"testing"
)

func TestRGBFromHex(t *testing.T) {
	tests := []struct {
		name        string
		hex         string
		r, g, b     int64
		isDefaulted bool
	}{
		{name: "valid lowercase", hex: "ff0000", r: 255, g: 0, b: 0},
		{name: "valid uppercase", hex: "00FF00", r: 0, g: 255, b: 0},
		{name: "trim spaces", hex: " #0000FF ", r: 0, g: 0, b: 255},
		{name: "invalid", hex: "not", r: 255, g: 255, b: 255, isDefaulted: true},
		{name: "too short", hex: "123", r: 255, g: 255, b: 255, isDefaulted: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, g, b := rgbFromHex(tc.hex)
			if r != tc.r || g != tc.g || b != tc.b {
				t.Fatalf("rgbFromHex(%q) = (%d,%d,%d), want (%d,%d,%d)", tc.hex, r, g, b, tc.r, tc.g, tc.b)
			}
			if tc.isDefaulted && (r != 255 || g != 255 || b != 255) {
				t.Fatal("expected defaulted white when input is invalid")
			}
		})
	}
}

func TestThemeStyle(t *testing.T) {
	theme := NewTheme(true)
	styled := theme.Style("hello", theme.Bold, theme.FGStrong)
	if styled == "hello" {
		t.Fatal("expected style to wrap text when enabled")
	}
	if !strings.HasSuffix(styled, theme.Reset) {
		t.Fatalf("expected style output to reset")
	}
	if !strings.HasPrefix(styled, theme.Bold+theme.FGStrong) {
		t.Fatalf("unexpected style prefix: %q", styled)
	}

	disabled := Theme{Enabled: false}
	if got := disabled.Style("hello", disabled.Bold); got != "hello" {
		t.Fatalf("expected no style when disabled, got %q", got)
	}
}
