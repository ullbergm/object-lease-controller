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
		// Edge cases
		"   secret1   ,   secret2   ", // heavy whitespace
		"secret-with-dashes,secret_with_underscores,secret.with.dots",
		"secret1,,,,secret2",            // multiple consecutive commas
		",,,",                           // only commas
		"                    ",          // only spaces
		"secret1\n,secret2\t,secret3\r", // various whitespace chars
		"very-long-secret-name-that-exceeds-typical-kubernetes-naming-conventions-but-should-still-parse",
		"SECRET1,SECRET2,SECRET3", // uppercase
		"123,456,789",             // numeric-only names
		"secret with spaces, another with  multiple  spaces", // spaces in names
		"s",                            // single char
		strings.Repeat("secret,", 100), // many items
		"secret1,",                     // trailing comma
		",secret1",                     // leading comma
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
