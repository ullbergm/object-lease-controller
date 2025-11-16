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
		// Edge cases
		"0s",
		"0d",
		"-0.5h",
		"1h30m45s",
		"1h30m45s100ms",
		"99999999999999999999999999999999999d", // overflow
		"1.00000000000001h",
		"   1h   ", // whitespace
		"1h\n",
		"1h\t",
		"100",    // no unit
		"h",      // no number
		"1hh",    // double unit
		"1h2h",   // duplicate time unit
		"1.2.3h", // malformed float
		"0.0000001ns",
		"-1w-2d", // negative compound
		"1e10h",  // scientific notation
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
