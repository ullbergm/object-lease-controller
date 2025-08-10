package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/runtime/schema"
	crmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

// LeaseMetrics holds Prometheus metrics for the lease controller for a specific GVK.
type LeaseMetrics struct {
	LeasesStarted     prometheus.Counter
	LeasesExpired     prometheus.Counter
	InvalidTTL        prometheus.Counter
	ReconcileErrors   prometheus.Counter
	ReconcileDuration prometheus.Histogram
}

// NewLeaseMetrics registers and returns metrics scoped to a specific GVK via const labels.
func NewLeaseMetrics(gvk schema.GroupVersionKind) *LeaseMetrics {
	constLabels := prometheus.Labels{
		"group":   gvk.Group,
		"version": gvk.Version,
		"kind":    gvk.Kind,
	}

	m := &LeaseMetrics{
		LeasesStarted: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace:   "object_lease_controller",
			Name:        "leases_started_total",
			Help:        "Number of leases started (lease-start set)",
			ConstLabels: constLabels,
		}),
		LeasesExpired: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace:   "object_lease_controller",
			Name:        "leases_expired_total",
			Help:        "Number of leases that have expired",
			ConstLabels: constLabels,
		}),
		InvalidTTL: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace:   "object_lease_controller",
			Name:        "invalid_ttl_total",
			Help:        "Number of objects with invalid TTL annotation encountered",
			ConstLabels: constLabels,
		}),
		ReconcileErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace:   "object_lease_controller",
			Name:        "reconcile_errors_total",
			Help:        "Number of reconcile errors",
			ConstLabels: constLabels,
		}),
		ReconcileDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace:   "object_lease_controller",
			Name:        "reconcile_duration_seconds",
			Help:        "Duration of LeaseWatcher reconcile in seconds",
			Buckets:     prometheus.DefBuckets,
			ConstLabels: constLabels,
		}),
	}

	crmetrics.Registry.MustRegister(
		m.LeasesStarted,
		m.LeasesExpired,
		m.InvalidTTL,
		m.ReconcileErrors,
		m.ReconcileDuration,
	)

	return m
}
