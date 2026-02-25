package utils

import "testing"

func TestContextLeftPercent(t *testing.T) {
	tests := []struct {
		name        string
		value       string
		meta        map[string]string
		wantPercent float64
		wantKnown   bool
	}{
		{name: "percent input", value: "12.5%", wantPercent: 87.5, wantKnown: true},
		{name: "percent with spaces", value: " 99.95 ", wantPercent: 0.05, wantKnown: true},
		{name: "percent without suffix", value: "20", wantPercent: 80, wantKnown: true},
		{name: "meta tokens used", value: "", meta: map[string]string{"tokens_input_used": "24000", "tokens_available": "400000"}, wantPercent: 94, wantKnown: true},
		{name: "meta uses tokens_used fallback", value: "", meta: map[string]string{"tokens_used": "24000", "tokens_available": "400000"}, wantPercent: 94, wantKnown: true},
		{name: "meta used exceeds available", value: "", meta: map[string]string{"tokens_input_used": "48000", "tokens_available": "24000"}, wantPercent: 33.3333333333, wantKnown: true},
		{name: "invalid value", value: "abc", wantPercent: 0, wantKnown: false},
		{name: "unusable meta", value: "", meta: map[string]string{"tokens_input_used": "abc", "tokens_available": "400000"}, wantPercent: 0, wantKnown: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotPercent, ok := ContextLeftPercent(tc.value, tc.meta)
			if ok != tc.wantKnown {
				t.Fatalf("ContextLeftPercent(%q) known = %v, want %v", tc.value, ok, tc.wantKnown)
			}
			if !tc.wantKnown {
				return
			}
			if tc.wantPercent == 0 {
				if gotPercent != tc.wantPercent {
					t.Fatalf("ContextLeftPercent(%q) = %f, want %f", tc.value, gotPercent, tc.wantPercent)
				}
				return
			}
			if gotPercent < tc.wantPercent-0.0001 || gotPercent > tc.wantPercent+0.0001 {
				t.Fatalf("ContextLeftPercent(%q) = %f, want around %f", tc.value, gotPercent, tc.wantPercent)
			}
		})
	}
}

func TestParseFloatLoose(t *testing.T) {
	tests := []struct {
		name   string
		value  string
		want   float64
		wantOK bool
	}{
		{name: "integer", value: "123", want: 123, wantOK: true},
		{name: "decimal", value: "12.5", want: 12.5, wantOK: true},
		{name: "thousands separator", value: "1,234.5", want: 1234.5, wantOK: true},
		{name: "with spaces", value: "  99.9  ", want: 99.9, wantOK: true},
		{name: "blank", value: "  ", wantOK: false},
		{name: "invalid", value: "abc", wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseFloatLoose(tc.value)
			if ok != tc.wantOK {
				t.Fatalf("parseFloatLoose(%q) known = %v, want %v", tc.value, ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if got != tc.want {
				t.Fatalf("parseFloatLoose(%q) = %f, want %f", tc.value, got, tc.want)
			}
		})
	}
}

func TestParsePercent(t *testing.T) {
	tests := []struct {
		name   string
		value  string
		want   float64
		wantOK bool
	}{
		{name: "with percent", value: "75%", want: 75, wantOK: true},
		{name: "without suffix", value: " 42 ", want: 42, wantOK: true},
		{name: "invalid", value: "x%", wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parsePercent(tc.value)
			if ok != tc.wantOK {
				t.Fatalf("parsePercent(%q) known = %v, want %v", tc.value, ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if got != tc.want {
				t.Fatalf("parsePercent(%q) = %f, want %f", tc.value, got, tc.want)
			}
		})
	}
}
