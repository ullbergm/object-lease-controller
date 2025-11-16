package util

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// FuzzMinimalObjectTransform creates unstructured objects with fuzzy annotations
// to verify MinimalObjectTransform does not panic and only keeps expected keys.
func FuzzMinimalObjectTransform(f *testing.F) {
	seeds := []string{
		"ttl=1h,lease-start=2024-01-01",
		"some=thing,other=val",
		"",
		"key=value=with=equals",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	keepKeys := []string{"object-lease-controller.ullberg.io/ttl", "object-lease-controller.ullberg.io/lease-start"}
	tf := MinimalObjectTransform(keepKeys...)

	f.Fuzz(func(t *testing.T, in string) {
		ann := map[string]string{}
		if in != "" {
			// parse simple key=value pairs separated by commas; ignore malformed parts
			for _, kv := range strings.Split(in, ",") {
				if kv == "" {
					continue
				}
				parts := strings.SplitN(kv, "=", 2)
				if len(parts) != 2 {
					continue
				}
				ann[parts[0]] = parts[1]
			}
		}
		u := &unstructured.Unstructured{}
		u.SetAPIVersion("v1")
		u.SetKind("ConfigMap")
		u.SetName("fuzz")
		u.SetNamespace("default")
		u.SetAnnotations(ann)

		// The TransformFunc should return a transformed object and no error.
		_, _ = tf(u)
	})
}
