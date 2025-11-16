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
		// Edge cases
		"object-lease-controller.ullberg.io/ttl=", // empty value
		"=empty-key", // empty key
		"key1=val1,key2=val2,key3=val3,key4=val4,key5=val5",                                         // many annotations
		"object-lease-controller.ullberg.io/ttl=1h,object-lease-controller.ullberg.io/lease-start=", // mixed keep keys
		"   key   =   value   ", // whitespace
		"key=value\nwith\nnewlines",
		"very-long-annotation-key-name-that-exceeds-normal-limits=value",
		"key=very-long-annotation-value-" + strings.Repeat("x", 1000),
		"special!@#$chars=value",
		"key=special!@#$value",
		"duplicate=first,duplicate=second", // duplicate keys
		"key",                              // no equals sign
		"key=",                             // empty value with equals
		"=",                                // just equals
		",,,",                              // only commas
		"k1=v1,,k2=v2",                     // consecutive commas
		strings.Repeat("key=val,", 100),    // many key-value pairs
		"object-lease-controller.ullberg.io/TTL=1h", // case variation on keep key
		"OBJECT-LEASE-CONTROLLER.ULLBERG.IO/ttl=2d", // full uppercase domain
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
