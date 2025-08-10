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

type clientProvider interface {
	GetClient() client.Client
}

type LeaseWatcher struct {
	client.Client
	GVK         schema.GroupVersionKind
	Tracker     *util.NamespaceTracker
	Recorder    record.EventRecorder
	eventChan   chan util.NamespaceChangeEvent
	Annotations Annotations
}

type Annotations struct {
	TTL        string
	LeaseStart string
	ExpireAt   string
	Status     string
}

var (
	setupLog = controller_runtime.Log.WithName("setup")
)

// Only trigger reconcile when relevant annotations change
func leaseRelevantAnns(u *unstructured.Unstructured, annotations Annotations) map[string]string {
	anns := u.GetAnnotations()
	keys := []string{annotations.TTL, annotations.LeaseStart}
	result := map[string]string{}
	for _, k := range keys {
		if v, ok := anns[k]; ok {
			result[k] = v
		}
	}
	return result
}

func (r *LeaseWatcher) onlyWithTTLAnnotation() predicate.Funcs {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			obj, ok := e.Object.(*unstructured.Unstructured)
			if !ok {
				return false
			}
			anns := obj.GetAnnotations()
			_, has := anns[r.Annotations.TTL]
			return has
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldObj, ok1 := e.ObjectOld.(*unstructured.Unstructured)
			newObj, ok2 := e.ObjectNew.(*unstructured.Unstructured)
			if !ok1 || !ok2 {
				return false
			}
			old := leaseRelevantAnns(oldObj, r.Annotations)
			new := leaseRelevantAnns(newObj, r.Annotations)
			return !reflect.DeepEqual(old, new)
		},
		DeleteFunc:  func(e event.DeleteEvent) bool { return false },
		GenericFunc: func(e event.GenericEvent) bool { return false },
	}
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
	if anns[r.Annotations.TTL] == "" {
		cleaned := false
		for _, k := range []string{r.Annotations.ExpireAt, r.Annotations.Status, r.Annotations.LeaseStart} {
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
	if v, ok := anns[r.Annotations.LeaseStart]; ok && v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			start = t.UTC()
		} else {
			// reset invalid value
			anns[r.Annotations.LeaseStart] = now.Format(time.RFC3339)
			r.updateAnnotations(ctx, obj, map[string]string{r.Annotations.LeaseStart: anns[r.Annotations.LeaseStart]})
			if r.Recorder != nil {
				r.Recorder.Event(obj, "Warning", "LeaseStartReset", "Invalid lease-start, reset to now")
			}
		}
	} else {
		anns[r.Annotations.LeaseStart] = now.Format(time.RFC3339)
		r.updateAnnotations(ctx, obj, map[string]string{r.Annotations.LeaseStart: anns[r.Annotations.LeaseStart]})
		if r.Recorder != nil {
			r.Recorder.Event(obj, "Normal", "LeaseStarted", "Lease started")
		}
		start = now
	}

	ttl, err := util.ParseFlexibleDuration(anns[r.Annotations.TTL])
	if err != nil {
		message := fmt.Sprintf("Invalid TTL: %v", err)
		r.updateAnnotations(ctx, obj, map[string]string{r.Annotations.Status: message})
		if r.Recorder != nil {
			r.Recorder.Event(obj, "Warning", "InvalidTTL", message)
		}
		return controller_runtime.Result{}, nil
	}

	expireAt := start.Add(ttl)

	if now.After(expireAt) {
		leaseStatus := "Lease expired. Deleting object."
		r.updateAnnotations(ctx, obj, map[string]string{
			r.Annotations.ExpireAt: expireAt.Format(time.RFC3339),
			r.Annotations.Status:   leaseStatus,
		})
		log.Info("Deleting object due to expired lease", "name", obj.GetName())
		if r.Recorder != nil {
			r.Recorder.Event(obj, "Normal", "LeaseExpired", leaseStatus)
		}
		if err := util.DeleteWithUIDPrecondition(ctx, r.Client, obj); client.IgnoreNotFound(err) != nil {
			return controller_runtime.Result{}, err
		}
		return controller_runtime.Result{}, nil
	}

	leaseStatus := fmt.Sprintf("Lease active. Expires at %s UTC.", expireAt.Format(time.RFC3339))
	r.updateAnnotations(ctx, obj, map[string]string{
		r.Annotations.ExpireAt: expireAt.Format(time.RFC3339),
		r.Annotations.Status:   leaseStatus,
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
		}}, builder.WithPredicates(r.onlyWithTTLAnnotation())).
		Complete(r)
}

// handleNamespaceEvents listens for tracker events and triggers reconciliation for new namespaces
func (r *LeaseWatcher) handleNamespaceEvents(mgr clientProvider) {
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
						if _, has := anns[r.Annotations.TTL]; has {
							req := controller_runtime.Request{
								NamespacedName: client.ObjectKeyFromObject(&obj),
							}
							go func(req controller_runtime.Request) {
								ctx := context.Background()
								if _, err := r.Reconcile(ctx, req); err != nil {
									logger.FromContext(ctx).Error(err, "Reconcile failed", "object", req.NamespacedName)
								}
							}(req)
						}
					}
				}
			}
		}
	}
}
