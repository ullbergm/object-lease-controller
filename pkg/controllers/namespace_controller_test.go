// pkg/controllers/namespace_controller_test.go
package controllers

import (
	"context"
	"errors"
	"testing"

	"object-lease-controller/pkg/util"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return s
}

func newReq(name string) ctrl.Request {
	return ctrl.Request{NamespacedName: types.NamespacedName{Name: name}}
}

// erroringClient wraps a client and forces Get to return an error
type erroringClient struct {
	client crclient.Client
	err    error
}

func (e *erroringClient) GroupVersionKindFor(obj runtime.Object) (schema.GroupVersionKind, error) {
	return e.client.GroupVersionKindFor(obj)
}
func (e *erroringClient) IsObjectNamespaced(obj runtime.Object) (bool, error) {
	return e.client.IsObjectNamespaced(obj)
}
func (e *erroringClient) Get(ctx context.Context, key crclient.ObjectKey, obj crclient.Object, opts ...crclient.GetOption) error {
	return e.err
}
func (e *erroringClient) Create(ctx context.Context, obj crclient.Object, opts ...crclient.CreateOption) error {
	return e.client.Create(ctx, obj, opts...)
}
func (e *erroringClient) Delete(ctx context.Context, obj crclient.Object, opts ...crclient.DeleteOption) error {
	return e.client.Delete(ctx, obj, opts...)
}
func (e *erroringClient) Update(ctx context.Context, obj crclient.Object, opts ...crclient.UpdateOption) error {
	return e.client.Update(ctx, obj, opts...)
}
func (e *erroringClient) Patch(ctx context.Context, obj crclient.Object, patch crclient.Patch, opts ...crclient.PatchOption) error {
	return e.client.Patch(ctx, obj, patch, opts...)
}
func (e *erroringClient) DeleteAllOf(ctx context.Context, obj crclient.Object, opts ...crclient.DeleteAllOfOption) error {
	return e.client.DeleteAllOf(ctx, obj, opts...)
}
func (e *erroringClient) List(ctx context.Context, list crclient.ObjectList, opts ...crclient.ListOption) error {
	return e.client.List(ctx, list, opts...)
}
func (e *erroringClient) Status() crclient.SubResourceWriter { return e.client.Status() }
func (e *erroringClient) SubResource(s string) crclient.SubResourceClient {
	return e.client.SubResource(s)
}
func (e *erroringClient) Scheme() *runtime.Scheme     { return e.client.Scheme() }
func (e *erroringClient) RESTMapper() meta.RESTMapper { return e.client.RESTMapper() }

func TestReconcile_AddsNamespaceOnMatchingLabel(t *testing.T) {
	scheme := newScheme(t)
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "optin",
			Labels: map[string]string{"watch/enabled": "true"},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ns).Build()

	tracker := util.NewNamespaceTracker()

	r := &NamespaceReconciler{
		Client:     cl,
		LabelKey:   "watch/enabled",
		LabelValue: "true",
		Tracker:    tracker,
	}

	_, err := r.Reconcile(context.Background(), newReq("optin"))
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}

	if !tracker.TrackingNamespace("optin") {
		t.Fatalf("expected tracker to contain namespace optin")
	}
}

func TestReconcile_RemovesNamespaceOnMismatchedLabel(t *testing.T) {
	scheme := newScheme(t)
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "mismatch",
			Labels: map[string]string{"watch/enabled": "false"},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ns).Build()

	tracker := util.NewNamespaceTracker()
	tracker.AddNamespace("mismatch")

	r := &NamespaceReconciler{
		Client:     cl,
		LabelKey:   "watch/enabled",
		LabelValue: "true",
		Tracker:    tracker,
	}

	_, err := r.Reconcile(context.Background(), newReq("mismatch"))
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}

	if tracker.TrackingNamespace("mismatch") {
		t.Fatalf("expected tracker to not contain namespace mismatch")
	}
}

func TestReconcile_RemoveWhenLabelMissing(t *testing.T) {
	scheme := newScheme(t)
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "nolabel",
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ns).Build()

	tracker := util.NewNamespaceTracker()
	tracker.AddNamespace("nolabel") // ensure it stays tracked

	r := &NamespaceReconciler{
		Client:     cl,
		LabelKey:   "watch/enabled",
		LabelValue: "true",
		Tracker:    tracker,
	}

	_, err := r.Reconcile(context.Background(), newReq("nolabel"))
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}

	if tracker.TrackingNamespace("nolabel") {
		t.Fatalf("expected tracker to not contain namespace nolabel")
	}
}

func TestReconcile_NotFoundRemovesTracking(t *testing.T) {
	scheme := newScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build() // no objects

	tracker := util.NewNamespaceTracker()
	tracker.AddNamespace("ghost")

	r := &NamespaceReconciler{
		Client:     cl,
		LabelKey:   "watch/enabled",
		LabelValue: "true",
		Tracker:    tracker,
	}

	_, err := r.Reconcile(context.Background(), newReq("ghost"))
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}

	if tracker.TrackingNamespace("ghost") {
		t.Fatalf("expected tracker to remove namespace ghost after NotFound")
	}
}

func TestReconcile_ClientErrorBubblesUp(t *testing.T) {
	scheme := newScheme(t)
	base := fake.NewClientBuilder().WithScheme(scheme).Build()
	cl := &erroringClient{client: base, err: errors.New("boom")}

	tracker := util.NewNamespaceTracker()
	tracker.AddNamespace("ns")

	r := &NamespaceReconciler{
		Client:     cl,
		LabelKey:   "watch/enabled",
		LabelValue: "true",
		Tracker:    tracker,
	}

	_, err := r.Reconcile(context.Background(), newReq("ns"))
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	if !tracker.TrackingNamespace("ns") {
		t.Fatalf("expected tracker to still contain namespace ns on error")
	}
}
