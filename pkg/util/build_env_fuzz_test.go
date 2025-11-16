package util

import (
	"strings"
	"testing"
)

// FuzzBuildEnvFrom ensures buildEnvFrom handles comma-separated lists robustly
func FuzzBuildEnvFrom(f *testing.F) {
	seeds := []string{
		"",
		"secret1",
		"secret1,secret2",
		"secret1, ,secret2",
		",secret1,",
		"comma,separated,values",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, in string) {
		// Convert to []string and call buildEnvFrom
		parts := strings.Split(in, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		_ = buildEnvFrom(parts)
	})
}
