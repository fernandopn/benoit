package utils

import "testing"

func TestCompactWhitespace(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "empty", value: "", want: ""},
		{name: "single word", value: "hello", want: "hello"},
		{name: "leading and trailing", value: "  hello   world  ", want: "hello world"},
		{name: "new lines", value: "a\n b\t\tc", want: "a b c"},
		{name: "tabs and spaces", value: "a\tb\n\tc", want: "a b c"},
		{name: "unicode", value: "héllo   世界", want: "héllo 世界"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := CompactWhitespace(tc.value); got != tc.want {
				t.Fatalf("CompactWhitespace(%q) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}
