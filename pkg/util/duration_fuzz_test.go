package util

import "testing"

// FuzzParseFlexibleDuration exercises ParseFlexibleDuration with random inputs
// to improve coverage and expose edge-cases in parsing flexible durations.
func FuzzParseFlexibleDuration(f *testing.F) {
	seeds := []string{
		"4h",
		"2d",
		"1w",
		"1mo",
		"1y",
		"1.5h",
		"0.5d",
		"-3h",
		"10µs",
		"5μs",
		"abc",
		"",
		// Additional variations
		"1d12h",
		"1d 12h",
		"1mo2h",
		"2w3d4h",
		"1.25d",
		"1000000000000h", // very large value
		"10ns",
		"10ms",
		"5mth",
		"1month",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, in string) {
		// Call the parser; we only assert no panics and valid error handling.
		// The function may return an error for invalid inputs — that's ok.
		_ = func() error {
			_, err := ParseFlexibleDuration(in)
			return err
		}()
	})
}
