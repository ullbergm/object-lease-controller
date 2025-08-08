package controllers

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func makeObj(anns map[string]string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetAnnotations(anns)
	return u
}

func TestLeaseRelevantAnns(t *testing.T) {
	// TTL only
	u := makeObj(map[string]string{
		AnnTTL:  "1h",
		"other": "ignore",
	})
	got := leaseRelevantAnns(u)
	want := map[string]string{
		AnnTTL: "1h",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("leaseRelevantAnns = %v, want %v", got, want)
	}

	// TTL + lease-start both included
	u2 := makeObj(map[string]string{
		AnnTTL:        "30m",
		AnnLeaseStart: "2025-01-01T00:00:00Z",
		"x":           "y",
	})
	got2 := leaseRelevantAnns(u2)
	want2 := map[string]string{
		AnnTTL:        "30m",
		AnnLeaseStart: "2025-01-01T00:00:00Z",
	}
	if !reflect.DeepEqual(got2, want2) {
		t.Errorf("leaseRelevantAnns = %v, want %v", got2, want2)
	}

	// No relevant annotations
	u3 := makeObj(map[string]string{"foo": "bar"})
	got3 := leaseRelevantAnns(u3)
	if len(got3) != 0 {
		t.Errorf("leaseRelevantAnns(no anns) = %v, want empty", got3)
	}
}

func TestOnlyWithTTLAnnotation_Create(t *testing.T) {
	tests := []struct {
		name string
		anns map[string]string
		want bool
	}{
		{"has TTL", map[string]string{AnnTTL: "5m"}, true},
		{"has lease-start but no TTL", map[string]string{AnnLeaseStart: "2025-01-01T00:00:00Z"}, false},
		{"no TTL", map[string]string{"foo": "bar"}, false},
	}

	for _, tt := range tests {
		u := makeObj(tt.anns)
		ev := event.CreateEvent{Object: u}
		if got := OnlyWithTTLAnnotation.CreateFunc(ev); got != tt.want {
			t.Errorf("CreateFunc(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestOnlyWithTTLAnnotation_Update(t *testing.T) {
	baseOld := makeObj(map[string]string{AnnTTL: "1h", AnnLeaseStart: "2025-01-01T00:00:00Z"})
	baseNewSame := makeObj(map[string]string{AnnTTL: "1h", AnnLeaseStart: "2025-01-01T00:00:00Z", "other": "x"})
	changedTTL := makeObj(map[string]string{AnnTTL: "2h", AnnLeaseStart: "2025-01-01T00:00:00Z"})
	changedLeaseStart := makeObj(map[string]string{AnnTTL: "1h", AnnLeaseStart: "2025-01-01T01:00:00Z"})
	leaseStartAdded := makeObj(map[string]string{AnnTTL: "1h", AnnLeaseStart: "2025-01-01T00:00:00Z"})
	leaseStartRemoved := makeObj(map[string]string{AnnTTL: "1h"})
	noAnns := makeObj(nil)

	tests := []struct {
		name   string
		oldObj *unstructured.Unstructured
		newObj *unstructured.Unstructured
		want   bool
	}{
		{"TTL changed", baseOld, changedTTL, true},
		{"LeaseStart changed", baseOld, changedLeaseStart, true},
		{"LeaseStart added", leaseStartRemoved, leaseStartAdded, true},
		{"LeaseStart removed", baseOld, leaseStartRemoved, true},
		{"Untracked annotation changed", baseOld, baseNewSame, false},
		{"TTL removed", baseOld, noAnns, true},
		{"Neither has TTL", noAnns, noAnns, false},
	}

	for _, tt := range tests {
		ev := event.UpdateEvent{ObjectOld: tt.oldObj, ObjectNew: tt.newObj}
		if got := OnlyWithTTLAnnotation.UpdateFunc(ev); got != tt.want {
			t.Errorf("UpdateFunc(%s) = %v, want %v", tt.name, got, tt.want)
		}
	}

	// wrong-type case
	evBad := event.UpdateEvent{
		ObjectOld: &corev1.Pod{},
		ObjectNew: &corev1.Pod{},
	}
	if OnlyWithTTLAnnotation.UpdateFunc(evBad) {
		t.Errorf("UpdateFunc(wrong types) = true, want false")
	}
}

func TestOnlyWithTTLAnnotation_Delete_Generic(t *testing.T) {
	if OnlyWithTTLAnnotation.DeleteFunc(event.DeleteEvent{}) {
		t.Error("DeleteFunc always false")
	}
	if OnlyWithTTLAnnotation.GenericFunc(event.GenericEvent{}) {
		t.Error("GenericFunc always false")
	}
}
