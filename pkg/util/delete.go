package util

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func DeleteWithUIDPrecondition(ctx context.Context, c client.Client, obj client.Object) error {
	uid := obj.GetUID()
	opts := &client.DeleteOptions{
		Preconditions: &metav1.Preconditions{UID: &uid},
	}
	return c.Delete(ctx, obj, opts)
}
