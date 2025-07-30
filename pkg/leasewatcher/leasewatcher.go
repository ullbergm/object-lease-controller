package leasewatcher

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	controller_runtime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"object-lease-controller/pkg/util"
)

// Lease annotation keys
const (
	AnnTTL        = "object-lease-controller.ullberg.us/ttl"
	AnnExtendedAt = "object-lease-controller.ullberg.us/extended-at"
	AnnExpireAt   = "object-lease-controller.ullberg.us/expire-at"
	AnnStatus     = "object-lease-controller.ullberg.us/lease-status"
)

type LeaseWatcher struct {
	client.Client
	GVK      schema.GroupVersionKind
	Recorder record.EventRecorder
}

var (
	setupLog = controller_runtime.Log.WithName("setup")
)

// Only trigger reconcile when relevant annotations change
func leaseRelevantAnns(u *unstructured.Unstructured) map[string]string {
	anns := u.GetAnnotations()
	keys := []string{AnnTTL, AnnExtendedAt}
	result := map[string]string{}
	for _, k := range keys {
		if v, ok := anns[k]; ok {
			result[k] = v
		}
	}
	return result
}

var OnlyWithTTLAnnotation = predicate.Funcs{
	CreateFunc: func(e event.CreateEvent) bool {
		obj, ok := e.Object.(*unstructured.Unstructured)
		if !ok {
			return false
		}
		anns := obj.GetAnnotations()
		_, has := anns[AnnTTL]
		return has
	},
	UpdateFunc: func(e event.UpdateEvent) bool {
		oldObj, ok1 := e.ObjectOld.(*unstructured.Unstructured)
		newObj, ok2 := e.ObjectNew.(*unstructured.Unstructured)
		if !ok1 || !ok2 {
			return false
		}
		old := leaseRelevantAnns(oldObj)
		new := leaseRelevantAnns(newObj)
		return !reflect.DeepEqual(old, new)
	},
	DeleteFunc:  func(e event.DeleteEvent) bool { return false },
	GenericFunc: func(e event.GenericEvent) bool { return false },
}

func (r *LeaseWatcher) Reconcile(ctx context.Context, req controller_runtime.Request) (controller_runtime.Result, error) {
	log := log.FromContext(ctx).WithValues("GVK", r.GVK)
	log.Info("reconciling lease")

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(r.GVK)
	if err := r.Get(ctx, req.NamespacedName, obj); err != nil {
		if client.IgnoreNotFound(err) == nil {
			log.Info("object not found, skipping")
			return controller_runtime.Result{}, nil
		}
		log.Error(err, "failed to get object")
		return controller_runtime.Result{}, client.IgnoreNotFound(err)
	}
	anns := obj.GetAnnotations()
	if anns == nil || anns[AnnTTL] == "" {
		cleaned := false
		for _, k := range []string{AnnExpireAt, AnnStatus, AnnExtendedAt} {
			if _, exists := anns[k]; exists {
				delete(anns, k)
				cleaned = true
			}
		}
		if cleaned {
			log.Info("cleaned up lease annotations", "name", obj.GetName())
			base := obj.DeepCopy()
			obj.SetAnnotations(anns)
			_ = r.Patch(ctx, obj, client.MergeFrom(base))

			// Record the event
			if r.Recorder != nil {
				r.Recorder.Event(obj, "Normal", "LeaseAnnotationsCleaned", "Removed lease-related annotations as TTL is missing")
			}
		}
		return controller_runtime.Result{}, nil
	}

	// Lease logic
	var start time.Time
	now := time.Now().UTC()
	// Only set AnnExtendedAt if TTL is added after creation (i.e., object is not new and AnnExpireAt is missing)
	ct := obj.GetCreationTimestamp()
	isNew := ct.IsZero() || ct.UTC().Add(10*time.Second).After(now) // treat as new if just created (10s window)
	if anns[AnnTTL] != "" && anns[AnnExpireAt] == "" && anns[AnnExtendedAt] == "" {
		if !isNew {
			log.Info("Lease TTL added after creation, setting extended-at", "name", obj.GetName())
			anns[AnnExtendedAt] = now.Format(time.RFC3339)
			r.updateAnnotations(ctx, obj, map[string]string{
				AnnExtendedAt: anns[AnnExtendedAt],
			})
			start = now

			// Record the event
			if r.Recorder != nil {
				r.Recorder.Event(obj, "Normal", "LeaseAdded", "Lease has been added to an existing object.")
			}
		} else {
			// Record the event
			if r.Recorder != nil {
				r.Recorder.Event(obj, "Normal", "LeaseAdded", "Lease was added when the object was created.")
			}
		}
	} else if ext, ok := anns[AnnExtendedAt]; ok && ext != "" {
		t, err := time.Parse(time.RFC3339, ext)
		if err == nil {
			start = t.UTC()
		}

		// Record the event
		if r.Recorder != nil {
			r.Recorder.Event(obj, "Normal", "LeaseExtended", "Lease was extended.")
		}
	}
	if start.IsZero() {
		if ct.IsZero() {
			start = now
		} else {
			start = ct.UTC()
		}
	}

	ttl, err := util.ParseFlexibleDuration(anns[AnnTTL])
	if err != nil {
		message := fmt.Sprintf("Invalid TTL: %v", err)
		r.updateAnnotations(ctx, obj, map[string]string{
			AnnStatus: message,
		})

		// Record the event
		if r.Recorder != nil {
			r.Recorder.Event(obj, "Warning", "InvalidTTL", message)
		}
		return controller_runtime.Result{}, nil
	}
	expireAt := start.Add(ttl)

	if now.After(expireAt) {
		leaseStatus := "Lease expired. Deleting object."
		r.updateAnnotations(ctx, obj, map[string]string{
			AnnExpireAt: expireAt.Format(time.RFC3339),
			AnnStatus:   leaseStatus,
		})

		log.Info("Deleting object due to expired lease", "name", obj.GetName())
		_ = r.Delete(ctx, obj)

		// Record the event
		if r.Recorder != nil {
			r.Recorder.Event(obj, "Normal", "LeaseExpired", leaseStatus)
		}

		// Return empty result to stop further processing
		return controller_runtime.Result{}, nil
	}

	leaseStatus := fmt.Sprintf("Lease active. Expires at %s UTC.", expireAt.Format(time.RFC3339))

	r.updateAnnotations(ctx, obj, map[string]string{
		AnnExpireAt: expireAt.Format(time.RFC3339),
		AnnStatus:   leaseStatus,
	})

	return controller_runtime.Result{RequeueAfter: expireAt.Sub(now)}, nil
}

func (r *LeaseWatcher) updateAnnotations(ctx context.Context, obj *unstructured.Unstructured, newAnns map[string]string) {
	base := obj.DeepCopy()
	anns := obj.GetAnnotations()
	if anns == nil {
		anns = map[string]string{}
	}
	for k, v := range newAnns {
		anns[k] = v
	}
	obj.SetAnnotations(anns)
	_ = r.Patch(ctx, obj, client.MergeFrom(base))
}

func (r *LeaseWatcher) SetupWithManager(mgr manager.Manager) error {
	setupLog.Info("Setting up LeaseWatcher", "GVK", r.GVK)
	return controller_runtime.NewControllerManagedBy(mgr).
		For(&unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": fmt.Sprintf("%s/%s", r.GVK.Group, r.GVK.Version),
			"kind":       r.GVK.Kind,
		}}, builder.WithPredicates(OnlyWithTTLAnnotation)).
		Complete(r)
}
