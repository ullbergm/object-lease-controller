package util

import (
	"strings"
	"testing"
)

// FuzzParseCleanupJobConfig ensures ParseCleanupJobConfig behaves correctly
// with random annotation values and does not panic.
func FuzzParseCleanupJobConfig(f *testing.F) {
	// Seeds: valid and invalid combinations
	seeds := []struct{ onDelete, wait, timeout, ttl, backoff, env string }{
		{"my-scripts/backup.sh", "true", "10m", "300", "3", "aws-creds,db"},
		{"scripts/cleanup.sh", "false", "5m", "60", "1", ""},
		{"invalid-no-slash", "true", "not-a-duration", "abc", "x", "one,two"},
		{"", "", "", "", "", ""},
		// Edge cases
		{"cm/script", "TRUE", "0s", "0", "0", "secret1,secret2,secret3"},
		{"cm/script", "False", "99999h", "2147483647", "999", ""},                          // max values
		{"cm/script", "yes", "1h30m", "-1", "-100", "secret-with-dashes"},                  // negative values
		{"namespace/cm/script/extra", "no", "invalid", "not-a-number", "x", ",,,,"},        // too many slashes, commas
		{"cm/", "1", "", "", "", " , , "},                                                  // empty script, whitespace in env
		{"/script", "0", "1.5h", "1.5", "1.5", "secret with spaces"},                       // leading slash, decimals
		{"cm/script with spaces", "on", "1d", "100000000000000000000", "10", ""},           // spaces in ref, huge ttl
		{"CM/SCRIPT", "OFF", "1w", "300", "3", "SECRET1,SECRET2"},                          // uppercase
		{"cm/script\nwith\nnewlines", "true\n", "10m\t", "300 ", " 3", "\naws-creds\n,db"}, // whitespace chars
	}
	for _, s := range seeds {
		f.Add(s.onDelete, s.wait, s.timeout, s.ttl, s.backoff, s.env)
	}

	f.Fuzz(func(t *testing.T, onDelete, waitStr, timeoutStr, ttlStr, backoffStr, envs string) {
		t.Helper()

		annotations := map[string]string{}
		if onDelete != "" {
			annotations["on-delete-job"] = onDelete
		}
		if waitStr != "" {
			annotations["job-wait"] = waitStr
		}
		if timeoutStr != "" {
			annotations["job-timeout"] = timeoutStr
		}
		if ttlStr != "" {
			annotations["job-ttl"] = ttlStr
		}
		if backoffStr != "" {
			annotations["job-backoff-limit"] = backoffStr
		}
		if envs != "" {
			// Introduce a case with extra commas and whitespace
			envs := strings.ReplaceAll(envs, ",", ", ")
			annotations["job-env-secrets"] = envs
		}

		annotationKeys := map[string]string{
			"OnDeleteJob":       "on-delete-job",
			"JobServiceAccount": "job-service-account",
			"JobImage":          "job-image",
			"JobWait":           "job-wait",
			"JobTimeout":        "job-timeout",
			"JobTTL":            "job-ttl",
			"JobBackoffLimit":   "job-backoff-limit",
			"JobEnvSecrets":     "job-env-secrets",
		}

		// Call the parser - fuzz ensures no panic or unexpected behavior.
		// We do not make strict assertions here; unit tests already cover correctness.
		_, _ = ParseCleanupJobConfig(annotations, annotationKeys)
	})
}
