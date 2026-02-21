package controllers

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/events"
	controller_runtime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	logger "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	ometrics "object-lease-controller/pkg/metrics"
	"object-lease-controller/pkg/util"
)

type clientProvider interface {
	GetClient() client.Client
}

type LeaseWatcher struct {
	client.Client
	GVK         schema.GroupVersionKind
	Tracker     *util.NamespaceTracker
	Recorder    events.EventRecorder
	eventChan   chan util.NamespaceChangeEvent
	Annotations Annotations
	Metrics     *ometrics.LeaseMetrics
}

type Annotations struct {
	TTL        string
	LeaseStart string
	ExpireAt   string
	Status     string

	// Cleanup job annotations
	OnDeleteJob       string
	JobServiceAccount string
	JobImage          string
	JobWait           string
	JobTimeout        string
	JobTTL            string
	JobBackoffLimit   string
	JobEnvSecrets     string
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

func (r *LeaseWatcher) Reconcile(ctx context.Context, req controller_runtime.Request) (res controller_runtime.Result, retErr error) {
	start := time.Now()
	log := logger.FromContext(ctx).WithValues("GVK", r.GVK)
	defer func() {
		if r.Metrics != nil {
			r.Metrics.ReconcileDuration.Observe(time.Since(start).Seconds())
			if retErr != nil {
				r.Metrics.ReconcileErrors.Inc()
			}
		}
	}()
	log.Info("reconciling lease")

	// Namespace filter
	if r.Tracker != nil && !r.isNamespaceTracked(req.Namespace) {
		log.Info("namespace not tracked, skipping", "namespace", req.Namespace)
		return controller_runtime.Result{}, nil
	}

	// Get object
	obj, err := r.getObject(ctx, req.NamespacedName)
	if err != nil {
		// not found is not an error
		return controller_runtime.Result{}, client.IgnoreNotFound(err)
	}

	// If no TTL, clean and exit
	if r.noTTL(obj) {
		r.cleanupLeaseAnnotations(ctx, obj)
		return controller_runtime.Result{}, nil
	}

	now := time.Now().UTC()
	startAt := r.ensureLeaseStart(ctx, obj, now)

	ttl, err := util.ParseFlexibleDuration(obj.GetAnnotations()[r.Annotations.TTL])
	if err != nil {
		r.markInvalidTTL(ctx, obj, err)
		return controller_runtime.Result{}, nil
	}

	expireAt := startAt.Add(ttl)
	if now.After(expireAt) {
		return r.handleExpired(ctx, obj, expireAt)
	}

	return r.setActive(ctx, obj, expireAt, now), nil
}

// ---------- helpers ----------

func (r *LeaseWatcher) isNamespaceTracked(ns string) bool {
	for _, n := range r.Tracker.ListNamespaces() {
		if ns == n {
			return true
		}
	}
	return false
}

func (r *LeaseWatcher) getObject(ctx context.Context, key client.ObjectKey) (*unstructured.Unstructured, error) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(r.GVK)
	if err := r.Get(ctx, key, obj); err != nil {
		return nil, err
	}
	if obj.GetAnnotations() == nil {
		obj.SetAnnotations(map[string]string{})
	}
	return obj, nil
}

func (r *LeaseWatcher) noTTL(obj *unstructured.Unstructured) bool {
	anns := obj.GetAnnotations()
	return anns[r.Annotations.TTL] == ""
}

func (r *LeaseWatcher) cleanupLeaseAnnotations(ctx context.Context, obj *unstructured.Unstructured) {
	anns := obj.GetAnnotations()
	cleaned := false
	for _, k := range []string{r.Annotations.ExpireAt, r.Annotations.Status, r.Annotations.LeaseStart} {
		if _, ok := anns[k]; ok {
			delete(anns, k)
			cleaned = true
		}
	}
	if !cleaned {
		return
	}
	base := obj.DeepCopy()
	obj.SetAnnotations(anns)
	_ = r.Patch(ctx, obj, client.MergeFrom(base))
	if r.Recorder != nil {
		r.Recorder.Eventf(obj, nil, "Normal", "LeaseAnnotationsCleaned", "LeaseAnnotationsCleaned", "Removed lease annotations because TTL is missing")
	}
}

func (r *LeaseWatcher) ensureLeaseStart(ctx context.Context, obj *unstructured.Unstructured, now time.Time) time.Time {
	anns := obj.GetAnnotations()
	if v, ok := anns[r.Annotations.LeaseStart]; ok && v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t.UTC()
		}
		// invalid, reset
		anns[r.Annotations.LeaseStart] = now.Format(time.RFC3339)
		r.updateAnnotations(ctx, obj, map[string]string{r.Annotations.LeaseStart: anns[r.Annotations.LeaseStart]})
		if r.Recorder != nil {
			r.Recorder.Eventf(obj, nil, "Warning", "LeaseStartReset", "LeaseStartReset", "Invalid lease-start, reset to now")
		}
		return now
	}
	// missing, set
	anns[r.Annotations.LeaseStart] = now.Format(time.RFC3339)
	r.updateAnnotations(ctx, obj, map[string]string{r.Annotations.LeaseStart: anns[r.Annotations.LeaseStart]})
	if r.Recorder != nil {
		r.Recorder.Eventf(obj, nil, "Normal", "LeaseStarted", "LeaseStarted", "Lease started")
	}
	if r.Metrics != nil {
		r.Metrics.LeasesStarted.Inc()
	}
	return now
}

func (r *LeaseWatcher) markInvalidTTL(ctx context.Context, obj *unstructured.Unstructured, parseErr error) {
	msg := fmt.Sprintf("Invalid TTL: %v", parseErr)
	r.updateAnnotations(ctx, obj, map[string]string{r.Annotations.Status: msg})
	if r.Recorder != nil {
		r.Recorder.Eventf(obj, nil, "Warning", "InvalidTTL", "InvalidTTL", "%s", msg)
	}
	if r.Metrics != nil {
		r.Metrics.InvalidTTL.Inc()
	}
}

//nolint:unparam
func (r *LeaseWatcher) handleExpired(ctx context.Context, obj *unstructured.Unstructured, expireAt time.Time) (controller_runtime.Result, error) {
	log := logger.FromContext(ctx)
	leaseStatus := "Lease expired. Deleting object."
	r.updateAnnotations(ctx, obj, map[string]string{
		r.Annotations.ExpireAt: expireAt.Format(time.RFC3339),
		r.Annotations.Status:   leaseStatus,
	})
	if r.Recorder != nil {
		r.Recorder.Eventf(obj, nil, "Normal", "LeaseExpired", "LeaseExpired", "%s", leaseStatus)
	}
	if r.Metrics != nil {
		r.Metrics.LeasesExpired.Inc()
	}

	// Check for cleanup job configuration
	anns := obj.GetAnnotations()
	annotationKeys := map[string]string{
		"OnDeleteJob":       r.Annotations.OnDeleteJob,
		"JobServiceAccount": r.Annotations.JobServiceAccount,
		"JobImage":          r.Annotations.JobImage,
		"JobWait":           r.Annotations.JobWait,
		"JobTimeout":        r.Annotations.JobTimeout,
		"JobTTL":            r.Annotations.JobTTL,
		"JobBackoffLimit":   r.Annotations.JobBackoffLimit,
		"JobEnvSecrets":     r.Annotations.JobEnvSecrets,
	}

	config, err := util.ParseCleanupJobConfig(anns, annotationKeys)
	if err != nil {
		// Invalid configuration - log error, emit event, proceed with deletion
		log.Error(err, "Invalid cleanup job configuration")
		if r.Recorder != nil {
			r.Recorder.Eventf(obj, nil, "Warning", "CleanupJobConfigInvalid", "CleanupJobConfigInvalid", "Invalid cleanup job config: %v", err)
		}
	} else if config != nil {
		// Cleanup job is configured - attempt to create and optionally wait
		if err := r.executeCleanupJob(ctx, obj, config, expireAt); err != nil {
			log.Error(err, "Cleanup job execution failed")
			if r.Recorder != nil {
				r.Recorder.Eventf(obj, nil, "Warning", "CleanupJobFailed", "CleanupJobFailed", "Cleanup job failed: %v", err)
			}
			if r.Metrics != nil {
				r.Metrics.CleanupJobsFailed.Inc()
			}
		}
		// Always proceed with deletion regardless of cleanup job outcome
	}

	if err := util.DeleteWithUIDPrecondition(ctx, r.Client, obj); client.IgnoreNotFound(err) != nil {
		return controller_runtime.Result{}, err
	}
	return controller_runtime.Result{}, nil
}

// executeCleanupJob creates and optionally waits for a cleanup job
func (r *LeaseWatcher) executeCleanupJob(ctx context.Context, obj *unstructured.Unstructured, config *util.CleanupJobConfig, expireAt time.Time) error {
	log := logger.FromContext(ctx)
	jobStart := time.Now()

	// Parse lease start time
	anns := obj.GetAnnotations()
	leaseStartStr := anns[r.Annotations.LeaseStart]
	leaseStartedAt, err := time.Parse(time.RFC3339, leaseStartStr)
	if err != nil {
		leaseStartedAt = time.Now() // Fallback
	}

	// Create the cleanup job
	job, err := util.CreateCleanupJob(ctx, r.Client, obj, r.GVK, config, leaseStartedAt, expireAt)
	if err != nil {
		return fmt.Errorf("failed to create cleanup job: %w", err)
	}

	log.Info("Cleanup job created", "job", job.Name, "namespace", job.Namespace)
	if r.Recorder != nil {
		r.Recorder.Eventf(obj, nil, "Normal", "CleanupJobCreated", "CleanupJobCreated", "Created cleanup job: %s", job.Name)
	}
	if r.Metrics != nil {
		r.Metrics.CleanupJobsCreated.Inc()
	}

	// If wait is enabled, wait for job completion
	if config.Wait {
		log.Info("Waiting for cleanup job to complete", "job", job.Name, "timeout", config.Timeout)
		if err := util.WaitForJobCompletion(ctx, r.Client, job, config.Timeout); err != nil {
			// Job failed or timed out
			if r.Recorder != nil {
				r.Recorder.Eventf(obj, nil, "Warning", "CleanupJobTimeout", "CleanupJobTimeout", "Cleanup job did not complete: %v", err)
			}
			return fmt.Errorf("cleanup job did not complete: %w", err)
		}

		// Job completed successfully
		log.Info("Cleanup job completed successfully", "job", job.Name)
		if r.Recorder != nil {
			r.Recorder.Eventf(obj, nil, "Normal", "CleanupJobCompleted", "CleanupJobCompleted", "Cleanup job completed: %s", job.Name)
		}
		if r.Metrics != nil {
			r.Metrics.CleanupJobsCompleted.Inc()
			r.Metrics.CleanupJobDuration.Observe(time.Since(jobStart).Seconds())
		}
	} else {
		// Fire-and-forget mode
		log.Info("Cleanup job created in fire-and-forget mode", "job", job.Name)
	}

	return nil
}

func (r *LeaseWatcher) setActive(ctx context.Context, obj *unstructured.Unstructured, expireAt time.Time, now time.Time) controller_runtime.Result {
	status := fmt.Sprintf("Lease active. Expires at %s UTC.", expireAt.Format(time.RFC3339))
	r.updateAnnotations(ctx, obj, map[string]string{
		r.Annotations.ExpireAt: expireAt.Format(time.RFC3339),
		r.Annotations.Status:   status,
	})
	return controller_runtime.Result{RequeueAfter: expireAt.Sub(now)}
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

	// Initialize metrics if not set
	if r.Metrics == nil {
		r.Metrics = ometrics.NewLeaseMetrics(r.GVK)
	}

	// Set up tracker event listener
	if r.Tracker != nil {
		r.eventChan = make(chan util.NamespaceChangeEvent, 10)
		r.Tracker.RegisterListener(r.eventChan)
		go r.handleNamespaceEvents(mgr)
	}

	// Build a typed unstructured with GVK set; for core group, apiVersion is just Version (e.g., "v1")
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(r.GVK)

	return controller_runtime.NewControllerManagedBy(mgr).
		For(obj, builder.WithPredicates(r.onlyWithTTLAnnotation())).
		Complete(r)
}

// handleNamespaceEvents listens for tracker events and triggers reconciliation for new namespaces
func (r *LeaseWatcher) handleNamespaceEvents(mgr clientProvider) {
	for evt := range r.eventChan {
		if evt.Change == util.NamespaceAdded {
			k8sClient := mgr.GetClient()
			list := &unstructured.UnstructuredList{}
			// For listing, the Kind must be Kind+"List"
			list.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   r.GVK.Group,
				Version: r.GVK.Version,
				Kind:    r.GVK.Kind + "List",
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
