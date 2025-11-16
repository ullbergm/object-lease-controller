package main

import (
	"os"
	"testing"
)

// FuzzParseLeaderElectionConfig fuzzes parseLeaderElectionConfig via environment
// variations and leader election namespace combinations.
func FuzzParseLeaderElectionConfig(f *testing.F) {
	seeds := []struct{ envVal, namespace string }{
		{"true", ""},
		{"1", ""},
		{"false", ""},
		{"notabool", ""},
		{"true", "custom-ns"},
		{"", "default"},
		// Edge cases
		{"TRUE", "uppercase"},
		{"False", "mixedcase"},
		{"on", "on-value"},
		{"off", "off-value"},
		{"yes", "yes-value"},
		{"no", "no-value"},
		{"0", "zero-value"},
		{"", "very-long-namespace-name-exceeding-normal-kubernetes-limits"},
		{"true", "ns-with-special-chars-123"},
		{"false", ""},
		{"   true   ", "whitespace-ns"},
		{"true\n", "newline"},
		{"\ttrue", "tab"},
		{"enabled", "enabled-value"},
	}
	for _, s := range seeds {
		f.Add(s.envVal, s.namespace)
	}

	f.Fuzz(func(t *testing.T, envVal, namespace string) {
		t.Helper()

		// Preserve environment
		old := os.Getenv("LEASE_LEADER_ELECTION")
		defer os.Setenv("LEASE_LEADER_ELECTION", old)

		// Set env for this run
		_ = os.Setenv("LEASE_LEADER_ELECTION", envVal)

		// Provide a fake stat and readFile path so we do not rely on actual files.
		prevStat := statFn
		prevRead := readFileFn
		defer func() { statFn = prevStat; readFileFn = prevRead }()

		// If no namespace in args, allow stat to succeed and read a value
		statFn = func(name string) (os.FileInfo, error) { return nil, nil }
		readFileFn = func(filename string) ([]byte, error) { return []byte("fuzz-ns"), nil }

		// Call under fuzz - ensuring no panics and both branches are exercised
		_, _, _ = parseLeaderElectionConfig(false, namespace)
	})
}
