package controllers

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// FuzzLeaseRelevantAnns stimulates leaseRelevantAnns with random annotations and keys.
func FuzzLeaseRelevantAnns(f *testing.F) {
	seeds := []struct{ key, val string }{
		{"object-lease-controller.ullberg.io/ttl", "30m"},
		{"lease/ttl", "1h"},
		{"kubectl.k8s.io/some", "value"},
		{"", ""},
		// Edge cases
		{"object-lease-controller.ullberg.io/ttl", ""}, // empty value
		{"object-lease-controller.ullberg.io/", "no-suffix"},
		{"object-lease-controller.ullberg.io/ttl/extra", "path-segments"},
		{"other.domain.com/ttl", "different-domain"},
		{"OBJECT-LEASE-CONTROLLER.ULLBERG.IO/TTL", "uppercase"},
		{"/ttl", "leading-slash"},
		{"ttl", "no-domain"},
		{"object-lease-controller.ullberg.io/very-long-annotation-key-name-that-exceeds-normal-limits", "long-key"},
		{"special!@#$%^&*()chars", "invalid-chars"},
		{"object-lease-controller.ullberg.io/ttl\n", "newline-in-key"},
		{"   object-lease-controller.ullberg.io/ttl   ", "whitespace-around-key"},
	}
	for _, s := range seeds {
		f.Add(s.key, s.val)
	}

	f.Fuzz(func(t *testing.T, key, val string) {
		t.Helper()
		anns := map[string]string{}
		if key != "" {
			anns[key] = val
		}
		u := &unstructured.Unstructured{}
		u.SetAnnotations(anns)
		_ = leaseRelevantAnns(u, Annotations{TTL: key, LeaseStart: "lease-start"})
	})
}
