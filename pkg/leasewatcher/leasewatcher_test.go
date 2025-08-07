package leasewatcher

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
	u := makeObj(map[string]string{
		AnnTTL:        "1h",
		AnnExtendedAt: "foo",
		"other":       "ignore",
	})
	got := leaseRelevantAnns(u)
	want := map[string]string{
		AnnTTL:        "1h",
		AnnExtendedAt: "foo",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("leaseRelevantAnns = %v, want %v", got, want)
	}

	u2 := makeObj(map[string]string{"foo": "bar"})
	got2 := leaseRelevantAnns(u2)
	if len(got2) != 0 {
		t.Errorf("leaseRelevantAnns(no anns) = %v, want empty", got2)
	}
}

func TestOnlyWithTTLAnnotation_Create(t *testing.T) {
	tests := []struct {
		name string
		anns map[string]string
		want bool
	}{
		{"has TTL", map[string]string{AnnTTL: "5m"}, true},
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
	baseOld := makeObj(map[string]string{AnnTTL: "1h"})
	baseNewSame := makeObj(map[string]string{AnnTTL: "1h", "other": "x"})
	changedTTL := makeObj(map[string]string{AnnTTL: "2h"})
	addExtended := makeObj(map[string]string{AnnTTL: "1h", AnnExtendedAt: "now"})
	noAnns := makeObj(nil)

	tests := []struct {
		name   string
		oldObj *unstructured.Unstructured
		newObj *unstructured.Unstructured
		want   bool
	}{
		{"TTL changed", baseOld, changedTTL, true},
		{"ExtendedAt added", baseOld, addExtended, true},
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
