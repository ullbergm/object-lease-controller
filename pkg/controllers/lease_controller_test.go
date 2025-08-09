package controllers

import (
	"context"
	"object-lease-controller/pkg/util"
	"reflect"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	controller_runtime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

// helpers

type stubMgr struct{ c client.Client }

func (s stubMgr) GetClient() client.Client { return s.c }
func makeObj(anns map[string]string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetAnnotations(anns)
	return u
}

func setMeta(u *unstructured.Unstructured, gvk schema.GroupVersionKind, ns, name string) {
	u.SetGroupVersionKind(gvk)
	u.SetNamespace(ns)
	u.SetName(name)
}

func newWatcher(t *testing.T, gvk schema.GroupVersionKind, objs ...client.Object) (*LeaseWatcher, client.Client, *runtime.Scheme) {
	t.Helper()
	scheme := runtime.NewScheme()
	// Register object and list types for fake client Get/List
	scheme.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind + "List",
	}, &unstructured.UnstructuredList{})

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	return &LeaseWatcher{Client: cl, GVK: gvk}, cl, scheme
}

func waitUntil(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v", timeout)
}

func get(t *testing.T, cl client.Client, gvk schema.GroupVersionKind, ns, name string) *unstructured.Unstructured {
	t.Helper()
	out := &unstructured.Unstructured{}
	out.SetGroupVersionKind(gvk)
	if err := cl.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: name}, out); err != nil {
		t.Fatalf("get error: %v", err)
	}
	return out
}

// existing predicate tests retained

func TestLeaseRelevantAnns(t *testing.T) {
	u := makeObj(map[string]string{
		AnnTTL:  "1h",
		"other": "ignore",
	})
	got := leaseRelevantAnns(u)
	want := map[string]string{
		AnnTTL: "1h",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("leaseRelevantAnns = %v, want %v", got, want)
	}

	u2 := makeObj(map[string]string{
		AnnTTL:        "30m",
		AnnLeaseStart: "2025-01-01T00:00:00Z",
		"x":           "y",
	})
	got2 := leaseRelevantAnns(u2)
	want2 := map[string]string{
		AnnTTL:        "30m",
		AnnLeaseStart: "2025-01-01T00:00:00Z",
	}
	if !reflect.DeepEqual(got2, want2) {
		t.Errorf("leaseRelevantAnns = %v, want %v", got2, want2)
	}

	u3 := makeObj(map[string]string{"foo": "bar"})
	got3 := leaseRelevantAnns(u3)
	if len(got3) != 0 {
		t.Errorf("leaseRelevantAnns(no anns) = %v, want empty", got3)
	}
}

func TestOnlyWithTTLAnnotation_Create(t *testing.T) {
	tests := []struct {
		name string
		anns map[string]string
		want bool
	}{
		{"has TTL", map[string]string{AnnTTL: "5m"}, true},
		{"has lease-start but no TTL", map[string]string{AnnLeaseStart: "2025-01-01T00:00:00Z"}, false},
		{"no TTL", map[string]string{"foo": "bar"}, false},
	}

	for _, tt := range tests {
		u := makeObj(tt.anns)
		ev := event.CreateEvent{Object: u}
		if got := OnlyWithTTLAnnotation.CreateFunc(ev); got != tt.want {
			t.Errorf("CreateFunc(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestOnlyWithTTLAnnotation_Update(t *testing.T) {
	baseOld := makeObj(map[string]string{AnnTTL: "1h", AnnLeaseStart: "2025-01-01T00:00:00Z"})
	baseNewSame := makeObj(map[string]string{AnnTTL: "1h", AnnLeaseStart: "2025-01-01T00:00:00Z", "other": "x"})
	changedTTL := makeObj(map[string]string{AnnTTL: "2h", AnnLeaseStart: "2025-01-01T00:00:00Z"})
	changedLeaseStart := makeObj(map[string]string{AnnTTL: "1h", AnnLeaseStart: "2025-01-01T01:00:00Z"})
	leaseStartAdded := makeObj(map[string]string{AnnTTL: "1h", AnnLeaseStart: "2025-01-01T00:00:00Z"})
	leaseStartRemoved := makeObj(map[string]string{AnnTTL: "1h"})
	noAnns := makeObj(nil)

	tests := []struct {
		name   string
		oldObj *unstructured.Unstructured
		newObj *unstructured.Unstructured
		want   bool
	}{
		{"TTL changed", baseOld, changedTTL, true},
		{"LeaseStart changed", baseOld, changedLeaseStart, true},
		{"LeaseStart added", leaseStartRemoved, leaseStartAdded, true},
		{"LeaseStart removed", baseOld, leaseStartRemoved, true},
		{"Untracked annotation changed", baseOld, baseNewSame, false},
		{"TTL removed", baseOld, noAnns, true},
		{"Neither has TTL", noAnns, noAnns, false},
	}

	for _, tt := range tests {
		ev := event.UpdateEvent{ObjectOld: tt.oldObj, ObjectNew: tt.newObj}
		if got := OnlyWithTTLAnnotation.UpdateFunc(ev); got != tt.want {
			t.Errorf("UpdateFunc(%s) = %v, want %v", tt.name, got, tt.want)
		}
	}

	// wrong types
	evBad := event.UpdateEvent{
		ObjectOld: &corev1.Pod{},
		ObjectNew: &corev1.Pod{},
	}
	if OnlyWithTTLAnnotation.UpdateFunc(evBad) {
		t.Errorf("UpdateFunc(wrong types) = true, want false")
	}

}

func TestOnlyWithTTLAnnotation_Delete_Generic(t *testing.T) {
	if OnlyWithTTLAnnotation.DeleteFunc(event.DeleteEvent{}) {
		t.Error("DeleteFunc always false")
	}
	if OnlyWithTTLAnnotation.GenericFunc(event.GenericEvent{}) {
		t.Error("GenericFunc always false")
	}
}

// TTL change updates expire-at
func TestReconcile_UpdatesExpireAtWhenTTLChanges(t *testing.T) {
	ctx := context.Background()
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

	start := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	obj := &unstructured.Unstructured{}
	setMeta(obj, gvk, "default", "cm1")
	obj.SetAnnotations(map[string]string{
		AnnTTL:        "1h",
		AnnLeaseStart: start.Format(time.RFC3339),
	})

	r, cl, _ := newWatcher(t, gvk, obj)

	_, err := r.Reconcile(ctx, controller_runtime.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "cm1"}})
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}
	got := get(t, cl, gvk, "default", "cm1")
	exp1 := start.Add(1 * time.Hour).Format(time.RFC3339)
	if got.GetAnnotations()[AnnExpireAt] != exp1 {
		t.Fatalf("expire-at after TTL=1h = %q, want %q", got.GetAnnotations()[AnnExpireAt], exp1)
	}

	// change TTL to 2h
	anns := got.GetAnnotations()
	anns[AnnTTL] = "2h"
	got.SetAnnotations(anns)
	if err := cl.Update(ctx, got); err != nil {
		t.Fatalf("update error: %v", err)
	}

	_, err = r.Reconcile(ctx, controller_runtime.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "cm1"}})
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}
	got2 := get(t, cl, gvk, "default", "cm1")
	exp2 := start.Add(2 * time.Hour).Format(time.RFC3339)
	if got2.GetAnnotations()[AnnExpireAt] != exp2 {
		t.Fatalf("expire-at after TTL=2h = %q, want %q", got2.GetAnnotations()[AnnExpireAt], exp2)
	}
}

// Auto-start when lease-start missing
func TestReconcile_AutoStartSetsLeaseStartAndExpire(t *testing.T) {
	ctx := context.Background()
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

	obj := &unstructured.Unstructured{}
	setMeta(obj, gvk, "default", "cm2")
	obj.SetAnnotations(map[string]string{
		AnnTTL: "5m",
	})

	r, cl, _ := newWatcher(t, gvk, obj)

	before := time.Now().UTC()
	_, err := r.Reconcile(ctx, controller_runtime.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "cm2"}})
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}
	got := get(t, cl, gvk, "default", "cm2")

	ls, err := time.Parse(time.RFC3339, got.GetAnnotations()[AnnLeaseStart])
	if err != nil {
		t.Fatalf("lease-start parse error: %v", err)
	}
	after := time.Now().UTC()
	if ls.Before(before.Add(-2*time.Second)) || ls.After(after.Add(2*time.Second)) {
		t.Fatalf("lease-start not within now window: %v vs [%v,%v]", ls, before, after)
	}
	exp := ls.Add(5 * time.Minute).Format(time.RFC3339)
	if got.GetAnnotations()[AnnExpireAt] != exp {
		t.Fatalf("expire-at = %q, want %q", got.GetAnnotations()[AnnExpireAt], exp)
	}
}

// Invalid lease-start resets and updates expire
func TestReconcile_InvalidLeaseStartResetsAndUpdatesExpire(t *testing.T) {
	ctx := context.Background()
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

	obj := &unstructured.Unstructured{}
	setMeta(obj, gvk, "default", "cm3")
	obj.SetAnnotations(map[string]string{
		AnnTTL:        "10m",
		AnnLeaseStart: "not-a-time",
	})

	r, cl, _ := newWatcher(t, gvk, obj)

	_, err := r.Reconcile(ctx, controller_runtime.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "cm3"}})
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}
	got := get(t, cl, gvk, "default", "cm3")
	ls, err := time.Parse(time.RFC3339, got.GetAnnotations()[AnnLeaseStart])
	if err != nil {
		t.Fatalf("lease-start not reset to RFC3339: %v", err)
	}
	exp := ls.Add(10 * time.Minute).Format(time.RFC3339)
	if got.GetAnnotations()[AnnExpireAt] != exp {
		t.Fatalf("expire-at = %q, want %q", got.GetAnnotations()[AnnExpireAt], exp)
	}
}

// Invalid TTL writes status, no expire, no delete
func TestReconcile_InvalidTTL_StatusOnly_NoExpire_NoDelete(t *testing.T) {
	ctx := context.Background()
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

	obj := &unstructured.Unstructured{}
	setMeta(obj, gvk, "default", "cm4")
	obj.SetAnnotations(map[string]string{
		AnnTTL: "totally-wrong",
	})

	r, cl, _ := newWatcher(t, gvk, obj)

	_, err := r.Reconcile(ctx, controller_runtime.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "cm4"}})
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}
	got := get(t, cl, gvk, "default", "cm4")
	anns := got.GetAnnotations()
	if anns[AnnExpireAt] != "" {
		t.Fatalf("expire-at should be empty, got %q", anns[AnnExpireAt])
	}
	if !strings.Contains(anns[AnnStatus], "Invalid TTL") {
		t.Fatalf("lease-status should mention Invalid TTL, got %q", anns[AnnStatus])
	}
}

// TTL removal clears lease annotations
func TestReconcile_RemoveTTL_CleansLeaseAnnotations(t *testing.T) {
	ctx := context.Background()
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

	obj := &unstructured.Unstructured{}
	setMeta(obj, gvk, "default", "cm5")
	obj.SetAnnotations(map[string]string{
		AnnTTL:        "1h",
		AnnLeaseStart: time.Now().UTC().Format(time.RFC3339),
		AnnExpireAt:   time.Now().UTC().Add(1 * time.Hour).Format(time.RFC3339),
		AnnStatus:     "ok",
	})

	r, cl, _ := newWatcher(t, gvk, obj)

	// remove TTL
	cur := get(t, cl, gvk, "default", "cm5")
	anns := cur.GetAnnotations()
	delete(anns, AnnTTL)
	cur.SetAnnotations(anns)
	if err := cl.Update(ctx, cur); err != nil {
		t.Fatalf("update error: %v", err)
	}

	_, err := r.Reconcile(ctx, controller_runtime.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "cm5"}})
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}
	got := get(t, cl, gvk, "default", "cm5")
	out := got.GetAnnotations()
	if _, ok := out[AnnLeaseStart]; ok {
		t.Fatalf("lease-start should be cleaned")
	}
	if _, ok := out[AnnExpireAt]; ok {
		t.Fatalf("expire-at should be cleaned")
	}
	if _, ok := out[AnnStatus]; ok {
		t.Fatalf("lease-status should be cleaned")
	}
}

// Extend by deleting lease-start
func TestReconcile_ExtendByDeletingLeaseStart(t *testing.T) {
	ctx := context.Background()
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

	start := time.Now().UTC().Add(-1 * time.Minute).Truncate(time.Second)
	obj := &unstructured.Unstructured{}
	setMeta(obj, gvk, "default", "cm6")
	obj.SetAnnotations(map[string]string{
		AnnTTL:        "10m",
		AnnLeaseStart: start.Format(time.RFC3339),
	})

	r, cl, _ := newWatcher(t, gvk, obj)

	_, _ = r.Reconcile(ctx, controller_runtime.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "cm6"}})
	got := get(t, cl, gvk, "default", "cm6")
	oldExp, _ := time.Parse(time.RFC3339, got.GetAnnotations()[AnnExpireAt])

	anns := got.GetAnnotations()
	delete(anns, AnnLeaseStart)
	got.SetAnnotations(anns)
	if err := cl.Update(ctx, got); err != nil {
		t.Fatalf("update error: %v", err)
	}

	_, _ = r.Reconcile(ctx, controller_runtime.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "cm6"}})
	got2 := get(t, cl, gvk, "default", "cm6")
	newExp, _ := time.Parse(time.RFC3339, got2.GetAnnotations()[AnnExpireAt])
	if !newExp.After(oldExp) {
		t.Fatalf("new expire-at %v should be after old %v", newExp, oldExp)
	}
}

// Expired deletes the object
func TestReconcile_ExpiredDeletesObject(t *testing.T) {
	ctx := context.Background()
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

	obj := &unstructured.Unstructured{}
	setMeta(obj, gvk, "default", "cm7")
	obj.SetAnnotations(map[string]string{
		AnnTTL:        "1s",
		AnnLeaseStart: time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339),
	})

	r, cl, _ := newWatcher(t, gvk, obj)

	_, _ = r.Reconcile(ctx, controller_runtime.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "cm7"}})

	out := &unstructured.Unstructured{}
	out.SetGroupVersionKind(gvk)
	err := cl.Get(ctx, types.NamespacedName{Namespace: "default", Name: "cm7"}, out)
	if err == nil || !apierrors.IsNotFound(err) {
		t.Fatalf("expected NotFound after delete, got %v", err)
	}
}

// Idempotency: no change on repeated reconcile
func TestReconcile_Idempotent_NoChange(t *testing.T) {
	ctx := context.Background()
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

	start := time.Now().UTC().Add(30 * time.Minute).Truncate(time.Second)
	obj := &unstructured.Unstructured{}
	setMeta(obj, gvk, "default", "cm8")
	obj.SetAnnotations(map[string]string{
		AnnTTL:        "1h",
		AnnLeaseStart: start.Format(time.RFC3339),
	})

	r, cl, _ := newWatcher(t, gvk, obj)

	_, _ = r.Reconcile(ctx, controller_runtime.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "cm8"}})
	a1 := get(t, cl, gvk, "default", "cm8").GetAnnotations()

	time.Sleep(10 * time.Millisecond) // small delay
	_, _ = r.Reconcile(ctx, controller_runtime.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "cm8"}})
	a2 := get(t, cl, gvk, "default", "cm8").GetAnnotations()

	if !reflect.DeepEqual(a1, a2) {
		t.Fatalf("annotations changed between reconciles: %v vs %v", a1, a2)
	}
}

// RequeueAfter ≈ expire-at - now
func TestReconcile_RequeueAfterApproxTTL(t *testing.T) {
	ctx := context.Background()
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

	start := time.Now().UTC().Add(10 * time.Second).Truncate(time.Second)
	obj := &unstructured.Unstructured{}
	setMeta(obj, gvk, "default", "cm9")
	obj.SetAnnotations(map[string]string{
		AnnTTL:        "1h",
		AnnLeaseStart: start.Format(time.RFC3339),
	})

	r, cl, _ := newWatcher(t, gvk, obj)

	before := time.Now().UTC()
	res, err := r.Reconcile(ctx, controller_runtime.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "cm9"}})
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}
	got := get(t, cl, gvk, "default", "cm9")
	exp, _ := time.Parse(time.RFC3339, got.GetAnnotations()[AnnExpireAt])

	// expected ≈ exp - now
	elapsed := time.Since(before)
	expected := time.Until(exp)

	if res.RequeueAfter <= 0 {
		t.Fatalf("RequeueAfter should be positive, got %v", res.RequeueAfter)
	}
	// allow small drift
	diff := res.RequeueAfter - expected
	if diff < -3*time.Second || diff > (elapsed+3*time.Second) {
		t.Fatalf("RequeueAfter %v not close to expected %v (diff %v)", res.RequeueAfter, expected, diff)
	}
}

// NotFound returns no error
func TestReconcile_NotFound_NoError(t *testing.T) {
	ctx := context.Background()
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

	r, _, _ := newWatcher(t, gvk /* no objects */)

	_, err := r.Reconcile(ctx, controller_runtime.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "does-not-exist"}})
	if err != nil {
		t.Fatalf("expected nil error on not found, got %v", err)
	}
}

// Predicate guardrail: unrelated annotation change does not trigger
func TestPredicate_UnrelatedAnnotationChangeDoesNotTrigger(t *testing.T) {
	oldObj := makeObj(map[string]string{AnnTTL: "1h"})
	newObj := makeObj(map[string]string{AnnTTL: "1h", "unrelated": "x"})
	ev := event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}
	if OnlyWithTTLAnnotation.UpdateFunc(ev) {
		t.Fatalf("unrelated annotation change should not trigger")
	}
}

// helper: like newWatcher but with a tracker
func newWatcherWithTracker(t *testing.T, gvk schema.GroupVersionKind, tr *util.NamespaceTracker, objs ...client.Object) (*LeaseWatcher, client.Client) {
	t.Helper()
	r, cl, _ := newWatcher(t, gvk, objs...)
	r.Tracker = tr
	return r, cl
}

func TestReconcile_SkipsUntrackedNamespace(t *testing.T) {
	ctx := context.Background()
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

	// object lives in ns-a
	obj := &unstructured.Unstructured{}
	setMeta(obj, gvk, "ns-a", "cm-ns-a")
	start := time.Now().UTC().Add(30 * time.Minute).Truncate(time.Second)
	obj.SetAnnotations(map[string]string{
		AnnTTL:        "1h",
		AnnLeaseStart: start.Format(time.RFC3339),
	})

	// tracker only allows ns-b
	tr := util.NewNamespaceTracker()
	tr.AddNamespace("ns-b")

	r, cl := newWatcherWithTracker(t, gvk, tr, obj)

	// reconcile for ns-a should be skipped, so expire-at must remain empty
	_, err := r.Reconcile(ctx, controller_runtime.Request{
		NamespacedName: types.NamespacedName{Namespace: "ns-a", Name: "cm-ns-a"},
	})
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}

	got := get(t, cl, gvk, "ns-a", "cm-ns-a")
	if v := got.GetAnnotations()[AnnExpireAt]; v != "" {
		t.Fatalf("expire-at set for untracked namespace: %q", v)
	}
}

func TestReconcile_ProcessesTrackedNamespace(t *testing.T) {
	ctx := context.Background()
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

	// object lives in ns-b
	obj := &unstructured.Unstructured{}
	setMeta(obj, gvk, "ns-b", "cm-ns-b")
	start := time.Now().UTC().Add(30 * time.Minute).Truncate(time.Second)
	obj.SetAnnotations(map[string]string{
		AnnTTL:        "45m",
		AnnLeaseStart: start.Format(time.RFC3339),
	})

	tr := util.NewNamespaceTracker()
	tr.AddNamespace("ns-b")

	r, cl := newWatcherWithTracker(t, gvk, tr, obj)

	_, err := r.Reconcile(ctx, controller_runtime.Request{
		NamespacedName: types.NamespacedName{Namespace: "ns-b", Name: "cm-ns-b"},
	})
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}

	got := get(t, cl, gvk, "ns-b", "cm-ns-b")
	wantExp := start.Add(45 * time.Minute).Format(time.RFC3339)
	if got.GetAnnotations()[AnnExpireAt] != wantExp {
		t.Fatalf("expire-at = %q, want %q", got.GetAnnotations()[AnnExpireAt], wantExp)
	}
}

func TestReconcile_NoTrackerProcessesAllNamespaces(t *testing.T) {
	ctx := context.Background()
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

	// object in any namespace should be processed if tracker is nil
	obj := &unstructured.Unstructured{}
	setMeta(obj, gvk, "random-ns", "cm-any")
	start := time.Now().UTC().Add(15 * time.Minute).Truncate(time.Second)
	obj.SetAnnotations(map[string]string{
		AnnTTL:        "30m",
		AnnLeaseStart: start.Format(time.RFC3339),
	})

	r, cl, _ := newWatcher(t, gvk, obj) // Tracker nil

	_, err := r.Reconcile(ctx, controller_runtime.Request{
		NamespacedName: types.NamespacedName{Namespace: "random-ns", Name: "cm-any"},
	})
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}

	got := get(t, cl, gvk, "random-ns", "cm-any")
	wantExp := start.Add(30 * time.Minute).Format(time.RFC3339)
	if got.GetAnnotations()[AnnExpireAt] != wantExp {
		t.Fatalf("expire-at = %q, want %q", got.GetAnnotations()[AnnExpireAt], wantExp)
	}
}

func TestReconcile_NamespaceBecomesTrackedLater(t *testing.T) {
	ctx := context.Background()
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

	obj := &unstructured.Unstructured{}
	setMeta(obj, gvk, "late-ns", "cm-late")
	start := time.Now().UTC().Add(20 * time.Minute).Truncate(time.Second)
	obj.SetAnnotations(map[string]string{
		AnnTTL:        "40m",
		AnnLeaseStart: start.Format(time.RFC3339),
	})

	tr := util.NewNamespaceTracker() // initially empty
	r, cl := newWatcherWithTracker(t, gvk, tr, obj)

	// first reconcile should skip
	_, err := r.Reconcile(ctx, controller_runtime.Request{
		NamespacedName: types.NamespacedName{Namespace: "late-ns", Name: "cm-late"},
	})
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}
	if v := get(t, cl, gvk, "late-ns", "cm-late").GetAnnotations()[AnnExpireAt]; v != "" {
		t.Fatalf("expire-at should be empty before namespace is tracked, got %q", v)
	}

	// now track the namespace and reconcile again
	tr.AddNamespace("late-ns")
	_, err = r.Reconcile(ctx, controller_runtime.Request{
		NamespacedName: types.NamespacedName{Namespace: "late-ns", Name: "cm-late"},
	})
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}
	wantExp := start.Add(40 * time.Minute).Format(time.RFC3339)
	if v := get(t, cl, gvk, "late-ns", "cm-late").GetAnnotations()[AnnExpireAt]; v != wantExp {
		t.Fatalf("expire-at = %q, want %q", v, wantExp)
	}
}

func TestHandleNamespaceEvents_ProcessesAddedNamespace(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

	withTTL := &unstructured.Unstructured{}
	setMeta(withTTL, gvk, "ns-a", "cm-ttl")
	withTTL.SetAnnotations(map[string]string{AnnTTL: "30m"})

	withoutTTL := &unstructured.Unstructured{}
	setMeta(withoutTTL, gvk, "ns-a", "cm-no-ttl")

	nsB := &unstructured.Unstructured{}
	setMeta(nsB, gvk, "ns-b", "cm-b")
	nsB.SetAnnotations(map[string]string{AnnTTL: "15m"})

	r, cl, _ := newWatcher(t, gvk, withTTL, withoutTTL, nsB)

	r.eventChan = make(chan util.NamespaceChangeEvent, 1)
	go r.handleNamespaceEvents(stubMgr{c: cl})

	r.eventChan <- util.NamespaceChangeEvent{Namespace: "ns-a", Change: util.NamespaceAdded}
	close(r.eventChan)

	waitUntil(t, 2*time.Second, func() bool {
		obj := get(t, cl, gvk, "ns-a", "cm-ttl")
		ls := obj.GetAnnotations()[AnnLeaseStart]
		ea := obj.GetAnnotations()[AnnExpireAt]
		if ls == "" || ea == "" {
			return false
		}
		start, err1 := time.Parse(time.RFC3339, ls)
		exp, err2 := time.Parse(time.RFC3339, ea)
		return err1 == nil && err2 == nil && exp.Equal(start.Add(30*time.Minute))
	})

	if v := get(t, cl, gvk, "ns-a", "cm-no-ttl").GetAnnotations()[AnnExpireAt]; v != "" {
		t.Fatalf("unexpected expire-at for object without TTL: %q", v)
	}
	if v := get(t, cl, gvk, "ns-b", "cm-b").GetAnnotations()[AnnExpireAt]; v != "" {
		t.Fatalf("ns-b object should not be processed by ns-a event, got %q", v)
	}
}

func TestHandleNamespaceEvents_IgnoresNonAddedEvents(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

	obj := &unstructured.Unstructured{}
	setMeta(obj, gvk, "ns-x", "cm-x")
	obj.SetAnnotations(map[string]string{AnnTTL: "10m"})

	r, cl, _ := newWatcher(t, gvk, obj)

	r.eventChan = make(chan util.NamespaceChangeEvent, 1)
	go r.handleNamespaceEvents(stubMgr{c: cl})

	r.eventChan <- util.NamespaceChangeEvent{Namespace: "ns-x", Change: util.NamespaceRemoved}
	close(r.eventChan)

	time.Sleep(50 * time.Millisecond)
	if v := get(t, cl, gvk, "ns-x", "cm-x").GetAnnotations()[AnnExpireAt]; v != "" {
		t.Fatalf("non-Added event should not process objects, got expire-at=%q", v)
	}
}

func TestHandleNamespaceEvents_ProcessesMultipleObjectsInAddedNamespace(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

	a1 := &unstructured.Unstructured{}
	setMeta(a1, gvk, "ns-y", "a1")
	a1.SetAnnotations(map[string]string{AnnTTL: "5m"})

	a2 := &unstructured.Unstructured{}
	setMeta(a2, gvk, "ns-y", "a2")
	a2.SetAnnotations(map[string]string{AnnTTL: "1h"})

	a3 := &unstructured.Unstructured{} // no TTL, should be ignored
	setMeta(a3, gvk, "ns-y", "a3")
	a3.SetAnnotations(map[string]string{})

	r, cl, _ := newWatcher(t, gvk, a1, a2, a3)
	r.eventChan = make(chan util.NamespaceChangeEvent, 1)
	go r.handleNamespaceEvents(stubMgr{c: cl})

	r.eventChan <- util.NamespaceChangeEvent{Namespace: "ns-y", Change: util.NamespaceAdded}
	close(r.eventChan)

	waitUntil(t, 2*time.Second, func() bool {
		g1 := get(t, cl, gvk, "ns-y", "a1").GetAnnotations()[AnnExpireAt]
		g2 := get(t, cl, gvk, "ns-y", "a2").GetAnnotations()[AnnExpireAt]
		return g1 != "" && g2 != ""
	})
	if v := get(t, cl, gvk, "ns-y", "a3").GetAnnotations()[AnnExpireAt]; v != "" {
		t.Fatalf("object without TTL should be ignored, got expire-at=%q", v)
	}
}
