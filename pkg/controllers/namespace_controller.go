package controllers

import (
	"context"

	"object-lease-controller/pkg/util"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logger "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

type NamespaceReconciler struct {
	client.Client
	Log        logr.Logger
	LabelKey   string
	LabelValue string
	Tracker    *util.NamespaceTracker
	Recorder   record.EventRecorder
}

func (r *NamespaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return builder.ControllerManagedBy(mgr).
		For(&corev1.Namespace{}).
		WithEventFilter(predicate.Or(
			predicate.LabelChangedPredicate{},
			predicate.GenerationChangedPredicate{},
		)).
		Complete(r)
}

func (r *NamespaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var ns corev1.Namespace
	log := logger.FromContext(ctx).WithValues("namespace", req.Name)

	if err := r.Get(ctx, req.NamespacedName, &ns); err != nil {
		if apierrors.IsNotFound(err) {
			log.V(2).Info("Namespace not found, not tracking", "namespace", req.Name)
			r.Tracker.RemoveNamespace(req.Name)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if ns.Labels[r.LabelKey] == r.LabelValue {
		log.Info("Namespace label matches, tracking")
		r.Tracker.AddNamespace(req.Name)
	} else {
		if _, exists := ns.Labels[r.LabelKey]; exists {
			log.V(2).Info("Namespace label exists but does not match, not tracking", "labelKey", r.LabelKey, "labelValue", r.LabelValue)
		} else {
			log.V(2).Info("Namespace label does not exist, not tracking", "labelKey", r.LabelKey)
		}
		r.Tracker.RemoveNamespace(req.Name)
	}
	return ctrl.Result{}, nil
}
