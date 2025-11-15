package util

import (
	"testing"
	"time"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestMinimalObjectTransform_UnstructuredPreservesExpectedFields(t *testing.T) {
	t.Parallel()

	u := &unstructured.Unstructured{}
	u.SetAPIVersion("apps/v1")
	u.SetKind("Deployment")
	u.SetName("nginx")
	u.SetNamespace("default")
	u.SetUID("abc123")
	u.SetAnnotations(map[string]string{"keep": "yes", "drop": "no"})

	tf := MinimalObjectTransform("keep")

	res, err := tf(u)
	if err != nil {
		t.Fatalf("transform error: %v", err)
	}

	out, ok := res.(*unstructured.Unstructured)
	if !ok {
		t.Fatalf("expected *unstructured.Unstructured, got %T", res)
	}

	// Fields should be preserved
	if out.GetName() != "nginx" || out.GetNamespace() != "default" || out.GetAPIVersion() != "apps/v1" {
		t.Fatalf("basic metadata not preserved: %s/%s %s", out.GetNamespace(), out.GetName(), out.GetAPIVersion())
	}

	if _, ok := out.GetAnnotations()["keep"]; !ok {
		t.Fatalf("expected 'keep' annotation to be preserved")
	}
	if _, ok := out.GetAnnotations()["drop"]; ok {
		t.Fatalf("expected 'drop' annotation to be removed")
	}
}

func TestMinimalObjectTransform_UnstructuredList(t *testing.T) {
	t.Parallel()

	l := &unstructured.UnstructuredList{}
	u1 := unstructured.Unstructured{}
	u1.SetAPIVersion("v1")
	u1.SetKind("ConfigMap")
	u1.SetName("one")
	u1.SetNamespace("ns")
	u1.SetUID("u1")
	u1.SetAnnotations(map[string]string{"keep": "x", "drop": "y"})

	u2 := unstructured.Unstructured{}
	u2.SetAPIVersion("v1")
	u2.SetKind("ConfigMap")
	u2.SetName("two")
	u2.SetNamespace("ns")
	u2.SetUID("u2")
	u2.SetAnnotations(map[string]string{"keep": "x", "drop": "y"})

	l.Items = append(l.Items, u1, u2)

	tf := MinimalObjectTransform("keep")
	res, err := tf(l)
	if err != nil {
		t.Fatalf("transform list error: %v", err)
	}

	out, ok := res.(*unstructured.UnstructuredList)
	if !ok {
		t.Fatalf("expected *unstructured.UnstructuredList, got %T", res)
	}
	if len(out.Items) != 2 {
		t.Fatalf("expected 2 items after transform, got %d", len(out.Items))
	}
	for _, item := range out.Items {
		if item.GetAnnotations()["keep"] != "x" {
			t.Fatalf("expected keep annotation preserved for item %s", item.GetName())
		}
		if _, ok := item.GetAnnotations()["drop"]; ok {
			t.Fatalf("expected drop annotation removed for item %s", item.GetName())
		}
	}
}

func TestMinimalObjectTransform_OtherTypes(t *testing.T) {
	t.Parallel()

	tf := MinimalObjectTransform("keep")
	// When passing an unsupported type, it should be returned unchanged
	s := "hello"
	res, err := tf(s)
	if err != nil {
		t.Fatalf("expected no error for non-k8s type, got %v", err)
	}
	if res != s {
		t.Fatalf("expected identity for non-k8s type, got %v", res)
	}
}

func TestStripU_PreservesDeletionTimestamp(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	u := &unstructured.Unstructured{}
	u.SetName("to-delete")
	u.SetNamespace("ns")
	// set deletion timestamp to simulate graceful deletion
	u.SetDeletionTimestamp(&v1.Time{Time: now})
	out := stripU(u, map[string]struct{}{})
	if out.GetDeletionTimestamp() == nil {
		t.Fatalf("expected deletion timestamp to be preserved")
	}
	if !out.GetDeletionTimestamp().Time.Equal(now) {
		t.Fatalf("deletion timestamp mismatch: got %v want %v", out.GetDeletionTimestamp(), now)
	}
}

func TestStripU_AnnotationFilteringMultipleAndDuplicate(t *testing.T) {
	t.Parallel()

	u := &unstructured.Unstructured{}
	u.SetName("multi")
	u.SetNamespace("ns")
	u.SetAnnotations(map[string]string{"one": "1", "two": "2", "three": "3"})

	// Build keep map with duplicates
	keys := []string{"one", "one", "two"}
	keep := make(map[string]struct{})
	for _, k := range keys {
		keep[k] = struct{}{}
	}

	out := stripU(u, keep)
	anns := out.GetAnnotations()
	if len(anns) != 2 {
		t.Fatalf("expected 2 annotations after filtering, got %d: %+v", len(anns), anns)
	}
	if anns["one"] != "1" || anns["two"] != "2" {
		t.Fatalf("annotations filtered incorrectly, got %+v", anns)
	}
	if _, ok := anns["three"]; ok {
		t.Fatalf("expected 'three' to be removed from annotations")
	}
}

func TestMinimalObjectTransform_StripsManagedFields(t *testing.T) {
	t.Parallel()

	u := &unstructured.Unstructured{}
	u.SetAPIVersion("v1")
	u.SetKind("ConfigMap")
	u.SetName("mf")
	u.SetNamespace("ns")
	// add a managed fields entry
	u.SetManagedFields([]v1.ManagedFieldsEntry{{Manager: "kube-apiserver"}})
	u.SetAnnotations(map[string]string{"keep": "yes"})

	tf := MinimalObjectTransform("keep")
	res, err := tf(u)
	if err != nil {
		t.Fatalf("transform error: %v", err)
	}
	out := res.(*unstructured.Unstructured)
	if len(out.GetManagedFields()) != 0 {
		t.Fatalf("expected managed fields to be stripped, got %v", out.GetManagedFields())
	}
}
