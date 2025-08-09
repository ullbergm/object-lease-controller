// pkg/util/delete_with_uid_precondition_test.go
package util

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// captureClient wraps a real client.Client, records Delete options, and can force an error.
type captureClient struct {
	client.Client // embedded to satisfy the interface

	deleteCalled int
	capturedOpts []client.DeleteOption
	forceErr     error
}

func (c *captureClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	c.deleteCalled++
	c.capturedOpts = append([]client.DeleteOption(nil), opts...)
	if c.forceErr != nil {
		return c.forceErr
	}
	return c.Client.Delete(ctx, obj, opts...)
}

func newScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("add to scheme: %v", err)
	}
	return s
}

func TestDeleteWithUIDPrecondition_SetsUID(t *testing.T) {
	t.Parallel()

	scheme := newScheme(t)

	uid := types.UID("12345")
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			UID:       uid,
		},
	}

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm.DeepCopy()).Build()
	cc := &captureClient{Client: base}

	ctx := context.Background()
	if err := DeleteWithUIDPrecondition(ctx, cc, cm); err != nil {
		t.Fatalf("DeleteWithUIDPrecondition error: %v", err)
	}

	if cc.deleteCalled != 1 {
		t.Fatalf("expected Delete to be called once, got %d", cc.deleteCalled)
	}

	// Reconstruct applied DeleteOptions from captured option(s) and assert UID precondition.
	var applied client.DeleteOptions
	for _, o := range cc.capturedOpts {
		o.ApplyToDelete(&applied)
	}
	if applied.Preconditions == nil || applied.Preconditions.UID == nil {
		t.Fatalf("expected UID precondition to be set, got nil")
	}
	if *applied.Preconditions.UID != uid {
		t.Fatalf("expected UID %q, got %q", uid, *applied.Preconditions.UID)
	}
}

func TestDeleteWithUIDPrecondition_PropagatesError(t *testing.T) {
	t.Parallel()

	scheme := newScheme(t)
	base := fake.NewClientBuilder().WithScheme(scheme).Build()
	cc := &captureClient{Client: base, forceErr: errors.New("boom")}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			UID:       types.UID("abc"),
		},
	}

	err := DeleteWithUIDPrecondition(context.Background(), cc, cm)
	if err == nil || err.Error() != "boom" {
		t.Fatalf("expected error 'boom', got %v", err)
	}

	// Ensure we still attempted a Delete and captured options.
	if cc.deleteCalled != 1 {
		t.Fatalf("expected Delete to be called once, got %d", cc.deleteCalled)
	}
	var applied client.DeleteOptions
	for _, o := range cc.capturedOpts {
		o.ApplyToDelete(&applied)
	}
	if applied.Preconditions == nil || applied.Preconditions.UID == nil {
		t.Fatalf("expected UID precondition to be set even on error")
	}
}
