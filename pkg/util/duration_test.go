package util

import (
	"testing"
	"time"
)

func TestParseFlexibleDuration(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
		wantErr  bool
	}{
		// simple units
		{"4h", 4 * time.Hour, false},
		{"2d", 48 * time.Hour, false},
		{"1w", 7 * 24 * time.Hour, false},
		{"1M", 30 * 24 * time.Hour, false},
		{"1y", 365 * 24 * time.Hour, false},

		// fractional
		{"1.5h", time.Duration(1.5 * float64(time.Hour)), false},
		{"0.5d", time.Duration(0.5 * 24 * float64(time.Hour)), false},

		// combinations (order and spaces)
		{"1w2d3h", (7*24 + 2*24 + 3) * time.Hour, false},
		{" 2d 4h ", (2*24 + 4) * time.Hour, false},

		// negative
		{"-3h", -3 * time.Hour, false},
		{"-1w2h", -(7*24*time.Hour + 2*time.Hour), false},

		// invalid
		{"", 0, true},
		{"abc", 0, true},
		{"10x", 0, true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseFlexibleDuration(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.input, err)
			}
			if got != tt.expected {
				t.Errorf("ParseFlexibleDuration(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}
