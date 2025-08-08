package controllers

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
	logger "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"object-lease-controller/pkg/util"
)

// Lease annotation keys
const (
	AnnTTL        = "object-lease-controller.ullberg.us/ttl"
	AnnLeaseStart = "object-lease-controller.ullberg.us/lease-start" // RFC3339 UTC
	AnnExpireAt   = "object-lease-controller.ullberg.us/expire-at"
	AnnStatus     = "object-lease-controller.ullberg.us/lease-status"
)

type LeaseWatcher struct {
	client.Client
	GVK       schema.GroupVersionKind
	Tracker   *util.NamespaceTracker
	Recorder  record.EventRecorder
	eventChan chan util.NamespaceChangeEvent
}

var (
	setupLog = controller_runtime.Log.WithName("setup")
)

// Only trigger reconcile when relevant annotations change
func leaseRelevantAnns(u *unstructured.Unstructured) map[string]string {
	anns := u.GetAnnotations()
	keys := []string{AnnTTL, AnnLeaseStart}
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
	log := logger.FromContext(ctx).WithValues("GVK", r.GVK)
	log.Info("reconciling lease")

	// Filter by tracker namespaces
	if r.Tracker != nil {
		namespaces := r.Tracker.ListNamespaces()
		found := false
		for _, ns := range namespaces {
			if req.Namespace == ns {
				found = true
				break
			}
		}
		if !found {
			log.Info("namespace not tracked, skipping", "namespace", req.Namespace)
			return controller_runtime.Result{}, nil
		}
	} else {
		log.Info("no namespace tracker configured, processing all namespaces")
	}

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
	if anns == nil {
		anns = map[string]string{}
	}

	// If TTL missing, clean up all lease annotations and return
	if anns[AnnTTL] == "" {
		cleaned := false
		for _, k := range []string{AnnExpireAt, AnnStatus, AnnLeaseStart} {
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
			if r.Recorder != nil {
				r.Recorder.Event(obj, "Normal", "LeaseAnnotationsCleaned", "Removed lease annotations because TTL is missing")
			}
		}
		return controller_runtime.Result{}, nil
	}

	now := time.Now().UTC()

	// Ensure lease-start exists. If missing or invalid, set to now.
	start := now
	if v, ok := anns[AnnLeaseStart]; ok && v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			start = t.UTC()
		} else {
			// reset invalid value
			anns[AnnLeaseStart] = now.Format(time.RFC3339)
			r.updateAnnotations(ctx, obj, map[string]string{AnnLeaseStart: anns[AnnLeaseStart]})
			if r.Recorder != nil {
				r.Recorder.Event(obj, "Warning", "LeaseStartReset", "Invalid lease-start, reset to now")
			}
		}
	} else {
		anns[AnnLeaseStart] = now.Format(time.RFC3339)
		r.updateAnnotations(ctx, obj, map[string]string{AnnLeaseStart: anns[AnnLeaseStart]})
		if r.Recorder != nil {
			r.Recorder.Event(obj, "Normal", "LeaseStarted", "Lease started")
		}
		start = now
	}

	ttl, err := util.ParseFlexibleDuration(anns[AnnTTL])
	if err != nil {
		message := fmt.Sprintf("Invalid TTL: %v", err)
		r.updateAnnotations(ctx, obj, map[string]string{AnnStatus: message})
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
		if r.Recorder != nil {
			r.Recorder.Event(obj, "Normal", "LeaseExpired", leaseStatus)
		}
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

	// Set up tracker event listener
	if r.Tracker != nil {
		r.eventChan = make(chan util.NamespaceChangeEvent, 10)
		r.Tracker.RegisterListener(r.eventChan)
		go r.handleNamespaceEvents(mgr)
	}

	return controller_runtime.NewControllerManagedBy(mgr).
		For(&unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": fmt.Sprintf("%s/%s", r.GVK.Group, r.GVK.Version),
			"kind":       r.GVK.Kind,
		}}, builder.WithPredicates(OnlyWithTTLAnnotation)).
		Complete(r)
}

// handleNamespaceEvents listens for tracker events and triggers reconciliation for new namespaces
func (r *LeaseWatcher) handleNamespaceEvents(mgr manager.Manager) {
	for evt := range r.eventChan {
		if evt.Change == util.NamespaceAdded {
			k8sClient := mgr.GetClient()
			list := &unstructured.UnstructuredList{}
			list.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   r.GVK.Group,
				Version: r.GVK.Version,
				Kind:    r.GVK.Kind,
			})
			opts := &client.ListOptions{Namespace: evt.Namespace}
			err := k8sClient.List(context.Background(), list, opts)
			if err == nil {
				for _, obj := range list.Items {
					anns := obj.GetAnnotations()
					if anns != nil {
						if _, has := anns[AnnTTL]; has {
							req := controller_runtime.Request{
								NamespacedName: client.ObjectKeyFromObject(&obj),
							}
							go r.Reconcile(context.Background(), req)
						}
					}
				}
			}
		}
	}
}
