package util

import (
	"strings"
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
		{"1mo", 30 * 24 * time.Hour, false},
		{"1Mo", 30 * 24 * time.Hour, false},
		{"1MO", 30 * 24 * time.Hour, false},
		{"1y", 365 * 24 * time.Hour, false},
		{"0.5mo", time.Duration(0.5 * 30 * 24 * float64(time.Hour)), false},

		// fractional
		{"1.5h", time.Duration(1.5 * float64(time.Hour)), false},
		{"0.5d", time.Duration(0.5 * 24 * float64(time.Hour)), false},
		{"20m", 20 * time.Minute, false},

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
		// missing unit
		{"10", 0, true},
		// micro symbols (greek and micro sign)
		{"10µs", 10 * time.Microsecond, false},
		{"5μs", 5 * time.Microsecond, false},
		// month aliases
		{"3mth", 3 * 30 * 24 * time.Hour, false},
		{"2month", 2 * 30 * 24 * time.Hour, false},
		// time.ParseDuration path with ns/ms
		{"15ms", 15 * time.Millisecond, false},
		{"100ns", 100 * time.Nanosecond, false},
		{"1mo2h", 30*24*time.Hour + 2*time.Hour, false},
	}

	for _, tt := range tests {

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

func TestParseFlexibleDuration_RangeErrors(t *testing.T) {
	t.Parallel()

	// Extremely large number should cause ParseDuration to fail with range error.
	big := strings.Repeat("9", 400)
	if _, err := ParseFlexibleDuration(big + "h"); err == nil {
		t.Fatalf("expected range error for extremely large duration number using time.ParseDuration")
	}

	// For custom units that use strconv.ParseFloat downstream, similarly expect an error
	if _, err := ParseFlexibleDuration(big + "d"); err == nil {
		t.Fatalf("expected range error for extremely large duration number using custom parse path")
	}
}
