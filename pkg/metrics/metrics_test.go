package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"k8s.io/apimachinery/pkg/runtime/schema"
	crmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

func withIsolatedRegistry(t *testing.T) *prometheus.Registry {
	t.Helper()
	old := crmetrics.Registry
	reg := prometheus.NewRegistry()
	crmetrics.Registry = reg
	t.Cleanup(func() { crmetrics.Registry = old })
	return reg
}

func findFamily(mfs []*dto.MetricFamily, name string) *dto.MetricFamily {
	for _, mf := range mfs {
		if mf != nil && mf.GetName() == name {
			return mf
		}
	}
	return nil
}

func labelsToMap(m *dto.Metric) map[string]string {
	out := map[string]string{}
	for _, lp := range m.GetLabel() {
		out[lp.GetName()] = lp.GetValue()
	}
	return out
}

func TestNewLeaseMetrics_RegistersAndLabels(t *testing.T) {
	reg := withIsolatedRegistry(t)

	gvk := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	m := NewLeaseMetrics(gvk)

	// Produce samples.
	m.LeasesStarted.Inc()    // 1
	m.LeasesExpired.Add(2)   // 2
	m.InvalidTTL.Inc()       // 1
	m.ReconcileErrors.Add(3) // 3
	m.ReconcileDuration.Observe(0.5)

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather failed: %v", err)
	}

	tests := []struct {
		name   string
		expect float64 // for counters
	}{
		{"object_lease_controller_leases_started_total", 1},
		{"object_lease_controller_leases_expired_total", 2},
		{"object_lease_controller_invalid_ttl_total", 1},
		{"object_lease_controller_reconcile_errors_total", 3},
		{"object_lease_controller_reconcile_duration_seconds", 0}, // histogram checked separately
	}

	for _, tt := range tests {
		mf := findFamily(mfs, tt.name)
		if mf == nil {
			t.Fatalf("missing metric family %q", tt.name)
		}
		if len(mf.Metric) != 1 {
			t.Fatalf("%q expected 1 metric, got %d", tt.name, len(mf.Metric))
		}
		lbls := labelsToMap(mf.Metric[0])
		if lbls["group"] != "apps" || lbls["version"] != "v1" || lbls["kind"] != "Deployment" {
			t.Fatalf("%q const labels mismatch: got %#v", tt.name, lbls)
		}

		// Counters
		if mf.GetType() == dto.MetricType_COUNTER && mf.Metric[0].GetCounter().GetValue() != tt.expect {
			t.Fatalf("%q value got %v want %v", tt.name, mf.Metric[0].GetCounter().GetValue(), tt.expect)
		}

		// Histogram
		if tt.name == "object_lease_controller_reconcile_duration_seconds" {
			h := mf.Metric[0].GetHistogram()
			if h.GetSampleCount() != 1 {
				t.Fatalf("histogram count got %v want 1", h.GetSampleCount())
			}
			if h.GetSampleSum() <= 0.0 {
				t.Fatalf("histogram sum should be > 0, got %v", h.GetSampleSum())
			}
		}
	}
}

func TestNewLeaseMetrics_DifferentGVKsCoexist(t *testing.T) {
	reg := withIsolatedRegistry(t)

	a := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	b := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

	ma := NewLeaseMetrics(a)
	mb := NewLeaseMetrics(b)

	ma.LeasesStarted.Inc()
	mb.LeasesStarted.Add(2)

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather failed: %v", err)
	}

	mf := findFamily(mfs, "object_lease_controller_leases_started_total")
	if mf == nil {
		t.Fatalf("missing leases_started_total")
	}
	if len(mf.Metric) != 2 {
		t.Fatalf("expected 2 metrics for leases_started_total, got %d", len(mf.Metric))
	}

	// Map metrics by labels to values.
	values := map[string]float64{}
	for _, m := range mf.Metric {
		lbls := labelsToMap(m)
		key := lbls["group"] + "|" + lbls["version"] + "|" + lbls["kind"]
		values[key] = m.GetCounter().GetValue()
	}
	if values["apps|v1|Deployment"] != 1 {
		t.Fatalf("apps/v1 Deployment value got %v want 1", values["apps|v1|Deployment"])
	}
	if values["|v1|ConfigMap"] != 2 {
		t.Fatalf("core/v1 ConfigMap value got %v want 2", values["|v1|ConfigMap"])
	}
}

func TestNewLeaseMetrics_DuplicateRegistrationPanics(t *testing.T) {
	_ = withIsolatedRegistry(t)

	gvk := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}

	didPanic := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				didPanic = true
			}
		}()
		_ = NewLeaseMetrics(gvk)
		// Second registration with identical const labels should panic.
		_ = NewLeaseMetrics(gvk)
	}()
	if !didPanic {
		t.Fatalf("expected panic on duplicate registration with same GVK")
	}
}
