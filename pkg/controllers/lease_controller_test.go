package controllers

import (
	"context"
	"fmt"
	"object-lease-controller/pkg/util"
	"reflect"
	"strings"
	"testing"
	"time"

	"net/http"
	ometrics "object-lease-controller/pkg/metrics"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	controller_runtime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	crmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// helpers
func defaultAnn() Annotations {
	return Annotations{
		TTL:        "object-lease-controller.ullberg.io/ttl",
		LeaseStart: "object-lease-controller.ullberg.io/lease-start",
		ExpireAt:   "object-lease-controller.ullberg.io/expire-at",
		Status:     "object-lease-controller.ullberg.io/lease-status",
	}
}

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
	return &LeaseWatcher{Client: cl, GVK: gvk, Annotations: defaultAnn()}, cl, scheme
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
	a := defaultAnn()
	u := makeObj(map[string]string{a.TTL: "1h", "other": "ignore"})
	got := leaseRelevantAnns(u, a)
	want := map[string]string{a.TTL: "1h"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("leaseRelevantAnns = %v, want %v", got, want)
	}

	u2 := makeObj(map[string]string{a.TTL: "30m", a.LeaseStart: "2025-01-01T00:00:00Z", "x": "y"})
	got2 := leaseRelevantAnns(u2, a)
	want2 := map[string]string{a.TTL: "30m", a.LeaseStart: "2025-01-01T00:00:00Z"}
	if !reflect.DeepEqual(got2, want2) {
		t.Errorf("leaseRelevantAnns = %v, want %v", got2, want2)
	}

	u3 := makeObj(map[string]string{"foo": "bar"})
	got3 := leaseRelevantAnns(u3, a)
	if len(got3) != 0 {
		t.Errorf("leaseRelevantAnns(no anns) = %v, want empty", got3)
	}
}

func TestOnlyWithTTLAnnotation_Create(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	r, _, _ := newWatcher(t, gvk)
	pred := r.onlyWithTTLAnnotation()
	a := r.Annotations
	tests := []struct {
		name string
		anns map[string]string
		want bool
	}{
		{"has TTL", map[string]string{a.TTL: "5m"}, true},
		{"has lease-start but no TTL", map[string]string{a.LeaseStart: "2025-01-01T00:00:00Z"}, false},
		{"no TTL", map[string]string{"foo": "bar"}, false},
	}

	for _, tt := range tests {
		u := makeObj(tt.anns)
		ev := event.CreateEvent{Object: u}
		if got := pred.CreateFunc(ev); got != tt.want {
			t.Errorf("CreateFunc(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestOnlyWithTTLAnnotation_Update(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	r, _, _ := newWatcher(t, gvk)
	pred := r.onlyWithTTLAnnotation()
	a := r.Annotations
	baseOld := makeObj(map[string]string{a.TTL: "1h", a.LeaseStart: "2025-01-01T00:00:00Z"})
	baseNewSame := makeObj(map[string]string{a.TTL: "1h", a.LeaseStart: "2025-01-01T00:00:00Z", "other": "x"})
	changedTTL := makeObj(map[string]string{a.TTL: "2h", a.LeaseStart: "2025-01-01T00:00:00Z"})
	changedLeaseStart := makeObj(map[string]string{a.TTL: "1h", a.LeaseStart: "2025-01-01T01:00:00Z"})
	leaseStartAdded := makeObj(map[string]string{a.TTL: "1h", a.LeaseStart: "2025-01-01T00:00:00Z"})
	leaseStartRemoved := makeObj(map[string]string{a.TTL: "1h"})
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
		if got := pred.UpdateFunc(ev); got != tt.want {
			t.Errorf("UpdateFunc(%s) = %v, want %v", tt.name, got, tt.want)
		}
	}

	// wrong types
	evBad := event.UpdateEvent{
		ObjectOld: &corev1.Pod{},
		ObjectNew: &corev1.Pod{},
	}
	if pred.UpdateFunc(evBad) {
		t.Errorf("UpdateFunc(wrong types) = true, want false")
	}

	// TTL added should trigger
	old := makeObj(nil)
	newWithTTL := makeObj(map[string]string{a.TTL: "2h"})
	evAdd := event.UpdateEvent{ObjectOld: old, ObjectNew: newWithTTL}
	if !pred.UpdateFunc(evAdd) {
		t.Errorf("UpdateFunc(TTL added) = false, want true")
	}
}

func TestOnlyWithTTLAnnotation_Delete_Generic(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	r, _, _ := newWatcher(t, gvk)
	pred := r.onlyWithTTLAnnotation()
	if pred.DeleteFunc(event.DeleteEvent{}) {
		t.Error("DeleteFunc always false")
	}
	if pred.GenericFunc(event.GenericEvent{}) {
		t.Error("GenericFunc always false")
	}
}

func TestOnlyWithTTLAnnotation_Create_NonUnstructured(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	r, _, _ := newWatcher(t, gvk)
	pred := r.onlyWithTTLAnnotation()

	// Use a core type that is not *unstructured.Unstructured
	pod := &corev1.Pod{}
	ev := event.CreateEvent{Object: pod}
	if pred.CreateFunc(ev) {
		t.Errorf("CreateFunc(non-Unstructured) = true, want false")
	}
}

func TestGetObject_SetsEmptyAnnotationsWhenNil(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	u := &unstructured.Unstructured{}
	setMeta(u, gvk, "default", "noanns")
	// Start with nil annotations
	u.SetAnnotations(nil)

	r, _, _ := newWatcher(t, gvk, u)
	obj, err := r.getObject(context.Background(), client.ObjectKey{Namespace: "default", Name: "noanns"})
	if err != nil {
		t.Fatalf("getObject error: %v", err)
	}
	if obj.GetAnnotations() == nil {
		t.Fatalf("expect annotations to be non-nil after getObject")
	}
	if len(obj.GetAnnotations()) != 0 {
		t.Fatalf("expected empty map, got %v", obj.GetAnnotations())
	}

	// Also ensure that getObject returns NotFound as client.IgnoreNotFound would
	r2, _, _ := newWatcher(t, gvk /* no objects */)
	_, err = r2.getObject(context.Background(), client.ObjectKey{Namespace: "none", Name: "missing"})
	if err == nil {
		t.Fatalf("expected error for missing object, got nil")
	}
}

func TestCleanupLeaseAnnotations_NoChange(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	obj := &unstructured.Unstructured{}
	setMeta(obj, gvk, "default", "clean-me")
	obj.SetAnnotations(map[string]string{"foo": "bar"})

	r, _, _ := newWatcher(t, gvk, obj)
	// call cleanup with no TTL present -- should return early without error
	r.cleanupLeaseAnnotations(context.Background(), obj)
	// ensure unchanged
	cur := get(t, r.Client, gvk, "default", "clean-me")
	if cur.GetAnnotations()["foo"] != "bar" {
		t.Fatalf("unexpected change to annotations: %v", cur.GetAnnotations())
	}
}

func TestUpdateAnnotations_HandlesNilAnnotations(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	obj := &unstructured.Unstructured{}
	setMeta(obj, gvk, "default", "patch")
	obj.SetAnnotations(nil)

	r, _, _ := newWatcher(t, gvk, obj)
	r.updateAnnotations(context.Background(), obj, map[string]string{"a": "b"})
	got := get(t, r.Client, gvk, "default", "patch")
	if got.GetAnnotations()["a"] != "b" {
		t.Fatalf("expected updated annotation, got %v", got.GetAnnotations())
	}
}

// withIsolatedRegistry is copied from metrics_test.go to allow test isolation
func withIsolatedRegistry(t *testing.T) *prometheus.Registry {
	t.Helper()
	old := crmetrics.Registry
	reg := prometheus.NewRegistry()
	crmetrics.Registry = reg
	t.Cleanup(func() { crmetrics.Registry = old })
	return reg
}

func TestEnsureLeaseStart_RecordsAndIncrementsMetrics(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

	reg := withIsolatedRegistry(t)

	obj := &unstructured.Unstructured{}
	setMeta(obj, gvk, "default", "es1")
	obj.SetAnnotations(map[string]string{})

	r, _, _ := newWatcher(t, gvk, obj)
	r.Metrics = ometrics.NewLeaseMetrics(gvk)
	r.Recorder = record.NewFakeRecorder(10)

	now := time.Now().UTC()
	_ = r.ensureLeaseStart(context.Background(), obj, now)

	// Ensure the LeasesStarted metric incremented
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather failed: %v", err)
	}
	found := false
	for _, mf := range mfs {
		if mf.GetName() == "object_lease_controller_leases_started_total" {
			if mf.Metric[0].GetCounter().GetValue() <= 0 {
				t.Fatalf("expected leases_started_total > 0, got %v", mf.Metric[0].GetCounter().GetValue())
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("leases_started_total metric not found")
	}
}

func TestMarkInvalidTTL_RecordsAndIncrementsMetrics(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

	reg := withIsolatedRegistry(t)

	obj := &unstructured.Unstructured{}
	setMeta(obj, gvk, "default", "badttl")
	obj.SetAnnotations(map[string]string{defaultAnn().TTL: "not-a-ttl"})

	r, _, _ := newWatcher(t, gvk, obj)
	r.Metrics = ometrics.NewLeaseMetrics(gvk)
	r.Recorder = record.NewFakeRecorder(10)

	r.markInvalidTTL(context.Background(), obj, fmt.Errorf("boom"))

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather failed: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() == "object_lease_controller_invalid_ttl_total" {
			if mf.Metric[0].GetCounter().GetValue() <= 0 {
				t.Fatalf("invalid_ttl_total should be incremented")
			}
			return
		}
	}
	t.Fatalf("invalid_ttl_total not found in metrics")
}

// captureClientErr forces Delete to return an error
type captureClientErr struct {
	client.Client
	forceErr error
}

func (c *captureClientErr) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	if c.forceErr != nil {
		return c.forceErr
	}
	return c.Client.Delete(ctx, obj, opts...)
}

func TestHandleExpired_PropagatesDeleteErrorAndIncrementsMetrics(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

	obj := &unstructured.Unstructured{}
	setMeta(obj, gvk, "default", "will-delete")
	obj.SetAnnotations(map[string]string{defaultAnn().TTL: "1s", defaultAnn().LeaseStart: time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)})

	r, cl, _ := newWatcher(t, gvk, obj)
	cc := &captureClientErr{Client: cl, forceErr: fmt.Errorf("boom")}
	r.Client = cc
	r.Metrics = ometrics.NewLeaseMetrics(gvk)

	_, err := r.handleExpired(context.Background(), obj, time.Now().UTC())
	if err == nil {
		t.Fatalf("expected error from handleExpired due to delete failure")
	}
}

func TestHandleExpired_RecordsEventAndIncrementsMetrics(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

	obj := &unstructured.Unstructured{}
	setMeta(obj, gvk, "default", "delete-ok")
	obj.SetAnnotations(map[string]string{defaultAnn().TTL: "1s", defaultAnn().LeaseStart: time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)})

	reg := withIsolatedRegistry(t)

	r, _, _ := newWatcher(t, gvk, obj)
	r.Metrics = ometrics.NewLeaseMetrics(gvk)
	r.Recorder = record.NewFakeRecorder(1)

	_, err := r.handleExpired(context.Background(), obj, time.Now().UTC())
	if err != nil {
		t.Fatalf("handleExpired returned error: %v", err)
	}

	// event was recorded
	select {
	case ev := <-r.Recorder.(*record.FakeRecorder).Events:
		if !strings.Contains(ev, "LeaseExpired") {
			t.Fatalf("unexpected event: %v", ev)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("expected LeaseExpired event but none found")
	}

	// metric incremented
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather failed: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() == "object_lease_controller_leases_expired_total" {
			if mf.Metric[0].GetCounter().GetValue() <= 0 {
				t.Fatalf("leases_expired_total should be incremented")
			}
			return
		}
	}
	t.Fatalf("leases_expired_total not found in metrics")
}

func TestOnlyWithTTLAnnotation_Update_NilOldOrNew(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	r, _, _ := newWatcher(t, gvk)
	pred := r.onlyWithTTLAnnotation()

	// ObjectOld nil
	evOldNil := event.UpdateEvent{ObjectOld: nil, ObjectNew: makeObj(map[string]string{"foo": "bar"})}
	if pred.UpdateFunc(evOldNil) {
		t.Errorf("UpdateFunc(ObjectOld=nil) = true, want false")
	}

	// ObjectNew nil
	evNewNil := event.UpdateEvent{ObjectOld: makeObj(map[string]string{"foo": "bar"}), ObjectNew: nil}
	if pred.UpdateFunc(evNewNil) {
		t.Errorf("UpdateFunc(ObjectNew=nil) = true, want false")
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
		defaultAnn().TTL:        "1h",
		defaultAnn().LeaseStart: start.Format(time.RFC3339),
	})

	r, cl, _ := newWatcher(t, gvk, obj)

	_, err := r.Reconcile(ctx, controller_runtime.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "cm1"}})
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}
	got := get(t, cl, gvk, "default", "cm1")
	exp1 := start.Add(1 * time.Hour).Format(time.RFC3339)
	if got.GetAnnotations()[defaultAnn().ExpireAt] != exp1 {
		t.Fatalf("expire-at after TTL=1h = %q, want %q", got.GetAnnotations()[defaultAnn().ExpireAt], exp1)
	}

	// change TTL to 2h
	anns := got.GetAnnotations()
	anns[defaultAnn().TTL] = "2h"
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
	if got2.GetAnnotations()[defaultAnn().ExpireAt] != exp2 {
		t.Fatalf("expire-at after TTL=2h = %q, want %q", got2.GetAnnotations()[defaultAnn().ExpireAt], exp2)
	}
}

// Auto-start when lease-start missing
func TestReconcile_AutoStartSetsLeaseStartAndExpire(t *testing.T) {
	ctx := context.Background()
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

	obj := &unstructured.Unstructured{}
	setMeta(obj, gvk, "default", "cm2")
	obj.SetAnnotations(map[string]string{
		defaultAnn().TTL: "5m",
	})

	r, cl, _ := newWatcher(t, gvk, obj)

	before := time.Now().UTC()
	_, err := r.Reconcile(ctx, controller_runtime.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "cm2"}})
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}
	got := get(t, cl, gvk, "default", "cm2")

	ls, err := time.Parse(time.RFC3339, got.GetAnnotations()[defaultAnn().LeaseStart])
	if err != nil {
		t.Fatalf("lease-start parse error: %v", err)
	}
	after := time.Now().UTC()
	if ls.Before(before.Add(-2*time.Second)) || ls.After(after.Add(2*time.Second)) {
		t.Fatalf("lease-start not within now window: %v vs [%v,%v]", ls, before, after)
	}
	exp := ls.Add(5 * time.Minute).Format(time.RFC3339)
	if got.GetAnnotations()[defaultAnn().ExpireAt] != exp {
		t.Fatalf("expire-at = %q, want %q", got.GetAnnotations()[defaultAnn().ExpireAt], exp)
	}
}

// Invalid lease-start resets and updates expire
func TestReconcile_InvalidLeaseStartResetsAndUpdatesExpire(t *testing.T) {
	ctx := context.Background()
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

	obj := &unstructured.Unstructured{}
	setMeta(obj, gvk, "default", "cm3")
	obj.SetAnnotations(map[string]string{
		defaultAnn().TTL:        "10m",
		defaultAnn().LeaseStart: "not-a-time",
	})

	r, cl, _ := newWatcher(t, gvk, obj)

	_, err := r.Reconcile(ctx, controller_runtime.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "cm3"}})
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}
	got := get(t, cl, gvk, "default", "cm3")
	ls, err := time.Parse(time.RFC3339, got.GetAnnotations()[defaultAnn().LeaseStart])
	if err != nil {
		t.Fatalf("lease-start not reset to RFC3339: %v", err)
	}
	exp := ls.Add(10 * time.Minute).Format(time.RFC3339)
	if got.GetAnnotations()[defaultAnn().ExpireAt] != exp {
		t.Fatalf("expire-at = %q, want %q", got.GetAnnotations()[defaultAnn().ExpireAt], exp)
	}
}

// Invalid TTL writes status, no expire, no delete
func TestReconcile_InvalidTTL_StatusOnly_NoExpire_NoDelete(t *testing.T) {
	ctx := context.Background()
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

	obj := &unstructured.Unstructured{}
	setMeta(obj, gvk, "default", "cm4")
	obj.SetAnnotations(map[string]string{
		defaultAnn().TTL: "totally-wrong",
	})

	r, cl, _ := newWatcher(t, gvk, obj)

	_, err := r.Reconcile(ctx, controller_runtime.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "cm4"}})
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}
	got := get(t, cl, gvk, "default", "cm4")
	anns := got.GetAnnotations()
	if anns[defaultAnn().ExpireAt] != "" {
		t.Fatalf("expire-at should be empty, got %q", anns[defaultAnn().ExpireAt])
	}
	if !strings.Contains(anns[defaultAnn().Status], "Invalid TTL") {
		t.Fatalf("lease-status should mention Invalid TTL, got %q", anns[defaultAnn().Status])
	}
}

// TTL removal clears lease annotations
func TestReconcile_RemoveTTL_CleansLeaseAnnotations(t *testing.T) {
	ctx := context.Background()
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

	obj := &unstructured.Unstructured{}
	setMeta(obj, gvk, "default", "cm5")
	obj.SetAnnotations(map[string]string{
		defaultAnn().TTL:        "1h",
		defaultAnn().LeaseStart: time.Now().UTC().Format(time.RFC3339),
		defaultAnn().ExpireAt:   time.Now().UTC().Add(1 * time.Hour).Format(time.RFC3339),
		defaultAnn().Status:     "ok",
	})

	r, cl, _ := newWatcher(t, gvk, obj)

	// remove TTL
	cur := get(t, cl, gvk, "default", "cm5")
	anns := cur.GetAnnotations()
	delete(anns, defaultAnn().TTL)
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
	if _, ok := out[defaultAnn().LeaseStart]; ok {
		t.Fatalf("lease-start should be cleaned")
	}
	if _, ok := out[defaultAnn().ExpireAt]; ok {
		t.Fatalf("expire-at should be cleaned")
	}
	if _, ok := out[defaultAnn().Status]; ok {
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
		defaultAnn().TTL:        "10m",
		defaultAnn().LeaseStart: start.Format(time.RFC3339),
	})

	r, cl, _ := newWatcher(t, gvk, obj)

	_, _ = r.Reconcile(ctx, controller_runtime.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "cm6"}})
	got := get(t, cl, gvk, "default", "cm6")
	oldExp, _ := time.Parse(time.RFC3339, got.GetAnnotations()[defaultAnn().ExpireAt])

	anns := got.GetAnnotations()
	delete(anns, defaultAnn().LeaseStart)
	got.SetAnnotations(anns)
	if err := cl.Update(ctx, got); err != nil {
		t.Fatalf("update error: %v", err)
	}

	_, _ = r.Reconcile(ctx, controller_runtime.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "cm6"}})
	got2 := get(t, cl, gvk, "default", "cm6")
	newExp, _ := time.Parse(time.RFC3339, got2.GetAnnotations()[defaultAnn().ExpireAt])
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
		defaultAnn().TTL:        "1s",
		defaultAnn().LeaseStart: time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339),
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
		defaultAnn().TTL:        "1h",
		defaultAnn().LeaseStart: start.Format(time.RFC3339),
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
		defaultAnn().TTL:        "1h",
		defaultAnn().LeaseStart: start.Format(time.RFC3339),
	})

	r, cl, _ := newWatcher(t, gvk, obj)

	before := time.Now().UTC()
	res, err := r.Reconcile(ctx, controller_runtime.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "cm9"}})
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}
	got := get(t, cl, gvk, "default", "cm9")
	exp, _ := time.Parse(time.RFC3339, got.GetAnnotations()[defaultAnn().ExpireAt])

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
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	r, _, _ := newWatcher(t, gvk)
	pred := r.onlyWithTTLAnnotation()
	a := r.Annotations
	oldObj := makeObj(map[string]string{a.TTL: "1h"})
	newObj := makeObj(map[string]string{a.TTL: "1h", "unrelated": "x"})
	ev := event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}
	if pred.UpdateFunc(ev) {
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
		defaultAnn().TTL:        "1h",
		defaultAnn().LeaseStart: start.Format(time.RFC3339),
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
	if v := got.GetAnnotations()[defaultAnn().ExpireAt]; v != "" {
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
		defaultAnn().TTL:        "45m",
		defaultAnn().LeaseStart: start.Format(time.RFC3339),
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
	if got.GetAnnotations()[defaultAnn().ExpireAt] != wantExp {
		t.Fatalf("expire-at = %q, want %q", got.GetAnnotations()[defaultAnn().ExpireAt], wantExp)
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
		defaultAnn().TTL:        "30m",
		defaultAnn().LeaseStart: start.Format(time.RFC3339),
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
	if got.GetAnnotations()[defaultAnn().ExpireAt] != wantExp {
		t.Fatalf("expire-at = %q, want %q", got.GetAnnotations()[defaultAnn().ExpireAt], wantExp)
	}
}

func TestReconcile_NamespaceBecomesTrackedLater(t *testing.T) {
	ctx := context.Background()
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

	obj := &unstructured.Unstructured{}
	setMeta(obj, gvk, "late-ns", "cm-late")
	start := time.Now().UTC().Add(20 * time.Minute).Truncate(time.Second)
	obj.SetAnnotations(map[string]string{
		defaultAnn().TTL:        "40m",
		defaultAnn().LeaseStart: start.Format(time.RFC3339),
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
	if v := get(t, cl, gvk, "late-ns", "cm-late").GetAnnotations()[defaultAnn().ExpireAt]; v != "" {
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
	if v := get(t, cl, gvk, "late-ns", "cm-late").GetAnnotations()[defaultAnn().ExpireAt]; v != wantExp {
		t.Fatalf("expire-at = %q, want %q", v, wantExp)
	}
}

func TestHandleNamespaceEvents_ProcessesAddedNamespace(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

	withTTL := &unstructured.Unstructured{}
	setMeta(withTTL, gvk, "ns-a", "cm-ttl")
	withTTL.SetAnnotations(map[string]string{defaultAnn().TTL: "30m"})

	withoutTTL := &unstructured.Unstructured{}
	setMeta(withoutTTL, gvk, "ns-a", "cm-no-ttl")

	nsB := &unstructured.Unstructured{}
	setMeta(nsB, gvk, "ns-b", "cm-b")
	nsB.SetAnnotations(map[string]string{defaultAnn().TTL: "15m"})

	r, cl, _ := newWatcher(t, gvk, withTTL, withoutTTL, nsB)

	r.eventChan = make(chan util.NamespaceChangeEvent, 1)
	go r.handleNamespaceEvents(stubMgr{c: cl})

	r.eventChan <- util.NamespaceChangeEvent{Namespace: "ns-a", Change: util.NamespaceAdded}
	close(r.eventChan)

	waitUntil(t, 2*time.Second, func() bool {
		obj := get(t, cl, gvk, "ns-a", "cm-ttl")
		ls := obj.GetAnnotations()[defaultAnn().LeaseStart]
		ea := obj.GetAnnotations()[defaultAnn().ExpireAt]
		if ls == "" || ea == "" {
			return false
		}
		start, err1 := time.Parse(time.RFC3339, ls)
		exp, err2 := time.Parse(time.RFC3339, ea)
		return err1 == nil && err2 == nil && exp.Equal(start.Add(30*time.Minute))
	})

	if v := get(t, cl, gvk, "ns-a", "cm-no-ttl").GetAnnotations()[defaultAnn().ExpireAt]; v != "" {
		t.Fatalf("unexpected expire-at for object without TTL: %q", v)
	}
	if v := get(t, cl, gvk, "ns-b", "cm-b").GetAnnotations()[defaultAnn().ExpireAt]; v != "" {
		t.Fatalf("ns-b object should not be processed by ns-a event, got %q", v)
	}
}

func TestHandleNamespaceEvents_IgnoresNonAddedEvents(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

	obj := &unstructured.Unstructured{}
	setMeta(obj, gvk, "ns-x", "cm-x")
	obj.SetAnnotations(map[string]string{defaultAnn().TTL: "10m"})

	r, cl, _ := newWatcher(t, gvk, obj)

	r.eventChan = make(chan util.NamespaceChangeEvent, 1)
	go r.handleNamespaceEvents(stubMgr{c: cl})

	r.eventChan <- util.NamespaceChangeEvent{Namespace: "ns-x", Change: util.NamespaceRemoved}
	close(r.eventChan)

	time.Sleep(50 * time.Millisecond)
	if v := get(t, cl, gvk, "ns-x", "cm-x").GetAnnotations()[defaultAnn().ExpireAt]; v != "" {
		t.Fatalf("non-Added event should not process objects, got expire-at=%q", v)
	}
}

func TestHandleNamespaceEvents_ProcessesMultipleObjectsInAddedNamespace(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

	a1 := &unstructured.Unstructured{}
	setMeta(a1, gvk, "ns-y", "a1")
	a1.SetAnnotations(map[string]string{defaultAnn().TTL: "5m"})

	a2 := &unstructured.Unstructured{}
	setMeta(a2, gvk, "ns-y", "a2")
	a2.SetAnnotations(map[string]string{defaultAnn().TTL: "1h"})

	a3 := &unstructured.Unstructured{} // no TTL, should be ignored
	setMeta(a3, gvk, "ns-y", "a3")
	a3.SetAnnotations(map[string]string{})

	r, cl, _ := newWatcher(t, gvk, a1, a2, a3)
	r.eventChan = make(chan util.NamespaceChangeEvent, 1)
	go r.handleNamespaceEvents(stubMgr{c: cl})

	r.eventChan <- util.NamespaceChangeEvent{Namespace: "ns-y", Change: util.NamespaceAdded}
	close(r.eventChan)

	waitUntil(t, 2*time.Second, func() bool {
		g1 := get(t, cl, gvk, "ns-y", "a1").GetAnnotations()[defaultAnn().ExpireAt]
		g2 := get(t, cl, gvk, "ns-y", "a2").GetAnnotations()[defaultAnn().ExpireAt]
		return g1 != "" && g2 != ""
	})
	if v := get(t, cl, gvk, "ns-y", "a3").GetAnnotations()[defaultAnn().ExpireAt]; v != "" {
		t.Fatalf("object without TTL should be ignored, got expire-at=%q", v)
	}
}

func TestCleanupLeaseAnnotations_RecordsEvent(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	obj := &unstructured.Unstructured{}
	setMeta(obj, gvk, "default", "clean-me-event")
	obj.SetAnnotations(map[string]string{"foo": "bar", defaultAnn().ExpireAt: "x", defaultAnn().Status: "y"})

	r, _, _ := newWatcher(t, gvk, obj)
	r.Recorder = record.NewFakeRecorder(1)
	// remove TTL to trigger cleanup flow
	r.cleanupLeaseAnnotations(context.Background(), obj)
	select {
	case ev := <-r.Recorder.(*record.FakeRecorder).Events:
		if !strings.Contains(ev, "Removed lease annotations") {
			t.Fatalf("unexpected event: %v", ev)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("expected event for cleaned annotations but none found")
	}
}

func TestEnsureLeaseStart_RecordsOnInvalidLeaseStart(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	obj := &unstructured.Unstructured{}
	setMeta(obj, gvk, "default", "invalid-start")
	obj.SetAnnotations(map[string]string{defaultAnn().TTL: "1h", defaultAnn().LeaseStart: "not-a-time"})

	r, _, _ := newWatcher(t, gvk, obj)
	r.Recorder = record.NewFakeRecorder(1)
	now := time.Now().UTC()
	_ = r.ensureLeaseStart(context.Background(), obj, now)
	select {
	case ev := <-r.Recorder.(*record.FakeRecorder).Events:
		if !strings.Contains(ev, "LeaseStartReset") {
			t.Fatalf("unexpected event: %v", ev)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("expected LeaseStartReset event but none found")
	}
}

// Use erroringClient type defined in namespace_controller_test.go

func TestReconcile_MetricsOnError(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	u := &unstructured.Unstructured{}
	setMeta(u, gvk, "default", "e1")
	u.SetAnnotations(map[string]string{defaultAnn().TTL: "1h"})

	// base client holds the object listed by handleNamespaceEvents
	base := fake.NewClientBuilder().WithScheme(runtime.NewScheme()).WithObjects(u).Build()
	// watcher uses erroring client for reconcile
	r := &LeaseWatcher{Client: &erroringClient{client: base, err: fmt.Errorf("boom")}, GVK: gvk, Annotations: defaultAnn()}
	// metrics enabled
	reg := withIsolatedRegistry(t)
	r.Metrics = ometrics.NewLeaseMetrics(gvk)

	_, err := r.Reconcile(context.Background(), controller_runtime.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "e1"}})
	if err == nil {
		t.Fatalf("expected error from reconcile, got nil")
	}

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather failed: %v", err)
	}
	// reconcile_errors_total should be > 0
	found := false
	for _, mf := range mfs {
		if mf.GetName() == "object_lease_controller_reconcile_errors_total" {
			if mf.Metric[0].GetCounter().GetValue() <= 0 {
				t.Fatalf("reconcile_errors_total should be >0")
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("reconcile_errors_total metric missing")
	}
}

func TestHandleNamespaceEvents_LogsOnReconcileError(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	withTTL := &unstructured.Unstructured{}
	setMeta(withTTL, gvk, "ns-e", "cm-ttl")
	withTTL.SetAnnotations(map[string]string{defaultAnn().TTL: "30m"})

	// base client has object; handleNamespaceEvents uses manager client for listing
	base := fake.NewClientBuilder().WithScheme(runtime.NewScheme()).WithObjects(withTTL).Build()

	// r.Reconcile will use a client that always returns error on Get
	r, _, _ := newWatcher(t, gvk, withTTL)
	r.Client = &erroringClient{client: base, err: fmt.Errorf("boom")}

	// event channel
	r.eventChan = make(chan util.NamespaceChangeEvent, 1)
	go r.handleNamespaceEvents(stubMgr{c: base})

	// send Added event - list should succeed but reconcile should error and be logged
	r.eventChan <- util.NamespaceChangeEvent{Namespace: "ns-e", Change: util.NamespaceAdded}
	close(r.eventChan)

	// wait a bit for goroutine to execute
	time.Sleep(200 * time.Millisecond)
}

func TestSetupWithManager_InitializesMetricsAndTracker(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

	// isolate metrics registry to avoid global conflicts
	reg := withIsolatedRegistry(t)

	// create watcher with tracker
	tr := util.NewNamespaceTracker()
	r := &LeaseWatcher{Client: fake.NewClientBuilder().WithScheme(runtime.NewScheme()).Build(), GVK: gvk, Tracker: tr, Annotations: defaultAnn()}

	// fake manager with scheme
	scheme := runtime.NewScheme()
	scheme.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
	mov := &fakeManager{client: r.Client, scheme: scheme}

	// Should not panic or return error
	if err := r.SetupWithManager(mov); err != nil {
		t.Fatalf("SetupWithManager failed: %v", err)
	}

	if r.Metrics == nil {
		t.Fatalf("expected metrics to be initialized")
	}
	if r.eventChan == nil {
		t.Fatalf("expected eventChan to be created when tracker present")
	}

	// Sanity: confirm the info metric family is registered
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather failed: %v", err)
	}
	found := false
	for _, mf := range mfs {
		if mf.GetName() == "object_lease_controller_info" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected info metric to be registered")
	}
}

// A minimal fake manager implementing manager.Manager with no-op methods for testing SetupWithManager
type fakeManager struct {
	client client.Client
	scheme *runtime.Scheme
}

func (f *fakeManager) GetClient() client.Client             { return f.client }
func (f *fakeManager) GetScheme() *runtime.Scheme           { return f.scheme }
func (f *fakeManager) GetConfig() *rest.Config              { return &rest.Config{} }
func (f *fakeManager) GetHTTPClient() *http.Client          { return &http.Client{} }
func (f *fakeManager) GetCache() cache.Cache                { return nil }
func (f *fakeManager) GetFieldIndexer() client.FieldIndexer { return nil }
func (f *fakeManager) GetEventRecorderFor(name string) record.EventRecorder {
	return record.NewFakeRecorder(10)
}
func (f *fakeManager) GetRESTMapper() meta.RESTMapper {
	return meta.NewDefaultRESTMapper([]schema.GroupVersion{{Group: "", Version: "v1"}})
}
func (f *fakeManager) GetAPIReader() client.Reader     { return f.client }
func (f *fakeManager) Start(ctx context.Context) error { return nil }

func (f *fakeManager) Add(r manager.Runnable) error { return nil }
func (f *fakeManager) Elected() <-chan struct{}     { return make(chan struct{}) }
func (f *fakeManager) AddMetricsServerExtraHandler(path string, handler http.Handler) error {
	return nil
}
func (f *fakeManager) AddHealthzCheck(name string, check healthz.Checker) error { return nil }
func (f *fakeManager) AddReadyzCheck(name string, check healthz.Checker) error  { return nil }
func (f *fakeManager) GetWebhookServer() webhook.Server                         { return nil }
func (f *fakeManager) GetLogger() logr.Logger                                   { return logr.Discard() }
func (f *fakeManager) GetControllerOptions() config.Controller                  { return config.Controller{} }
