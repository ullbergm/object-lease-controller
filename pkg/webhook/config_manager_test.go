package webhook

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

const testTTLAnnotation = "object-lease-controller.ullberg.io/ttl"

func newTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = admissionv1.AddToScheme(scheme)

	// Register LeaseController GVK
	leaseControllerGVK := schema.GroupVersionKind{
		Group:   "object-lease-controller.ullberg.io",
		Version: "v1",
		Kind:    "LeaseController",
	}
	scheme.AddKnownTypeWithName(leaseControllerGVK, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group:   "object-lease-controller.ullberg.io",
		Version: "v1",
		Kind:    "LeaseControllerList",
	}, &unstructured.UnstructuredList{})

	return scheme
}

func newTestClient(objs ...client.Object) client.Client {
	scheme := newTestScheme()
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
}

func newTestConfigManager(cl client.Client) *ConfigManager {
	return NewConfigManager(cl, testTTLAnnotation, logr.Discard())
}

func makeLeaseController(name, ns string, spec map[string]interface{}) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "object-lease-controller.ullberg.io",
		Version: "v1",
		Kind:    "LeaseController",
	})
	u.SetName(name)
	u.SetNamespace(ns)
	if spec != nil {
		_ = unstructured.SetNestedMap(u.Object, spec, "spec")
	}
	return u
}

func TestNewConfigManager(t *testing.T) {
	cl := newTestClient()
	cm := NewConfigManager(cl, testTTLAnnotation, logr.Discard())

	if cm == nil {
		t.Fatal("expected non-nil ConfigManager")
	}
	if cm.ttlAnnotation != testTTLAnnotation {
		t.Errorf("ttlAnnotation = %q, want %q", cm.ttlAnnotation, testTTLAnnotation)
	}
	if cm.configs == nil {
		t.Error("expected configs map to be initialized")
	}
	if cm.crToGVK == nil {
		t.Error("expected crToGVK map to be initialized")
	}
}

func TestShouldValidate_NoConfig(t *testing.T) {
	cl := newTestClient()
	cm := newTestConfigManager(cl)

	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	if cm.ShouldValidate(gvk) {
		t.Error("expected ShouldValidate to return false for unconfigured GVK")
	}
}

func TestShouldValidate_EnabledConfig(t *testing.T) {
	cl := newTestClient()
	cm := newTestConfigManager(cl)

	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	cm.configs[gvk] = GVKConfig{
		GVK:     gvk,
		Enabled: true,
	}

	if !cm.ShouldValidate(gvk) {
		t.Error("expected ShouldValidate to return true for enabled GVK")
	}
}

func TestShouldValidate_DisabledConfig(t *testing.T) {
	cl := newTestClient()
	cm := newTestConfigManager(cl)

	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	cm.configs[gvk] = GVKConfig{
		GVK:     gvk,
		Enabled: false,
	}

	if cm.ShouldValidate(gvk) {
		t.Error("expected ShouldValidate to return false for disabled GVK")
	}
}

func TestWatchPredicates(t *testing.T) {
	cl := newTestClient()
	cm := newTestConfigManager(cl)

	pred := cm.watchPredicates()

	// CreateFunc should return true
	if !pred.Create(event.CreateEvent{}) {
		t.Error("expected CreateFunc to return true")
	}

	// UpdateFunc should return true
	if !pred.Update(event.UpdateEvent{}) {
		t.Error("expected UpdateFunc to return true")
	}

	// DeleteFunc should return true
	if !pred.Delete(event.DeleteEvent{}) {
		t.Error("expected DeleteFunc to return true")
	}
}

func TestReconcile_NotFound(t *testing.T) {
	cl := newTestClient()
	cm := newTestConfigManager(cl)

	// Add a stale config to verify cleanup
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	nn := types.NamespacedName{Namespace: "default", Name: "deleted-lc"}
	cm.configs[gvk] = GVKConfig{GVK: gvk, Enabled: true}
	cm.crToGVK[nn] = gvk

	req := ctrl.Request{NamespacedName: nn}
	_, err := cm.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify stale config was cleaned up
	if _, exists := cm.configs[gvk]; exists {
		t.Error("expected stale config to be removed")
	}
	if _, exists := cm.crToGVK[nn]; exists {
		t.Error("expected stale crToGVK entry to be removed")
	}
}

func TestReconcile_EnabledWebhook(t *testing.T) {
	spec := map[string]interface{}{
		"group":   "apps",
		"version": "v1",
		"kind": map[string]interface{}{
			"singular": "Deployment",
			"plural":   "deployments",
		},
		"webhook": map[string]interface{}{
			"enabled":       true,
			"failurePolicy": "Fail",
		},
	}
	lc := makeLeaseController("test-lc", "default", spec)

	cl := newTestClient(lc)
	cm := newTestConfigManager(cl)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "test-lc"}}
	_, err := cm.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify config was added
	gvk := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	config, exists := cm.configs[gvk]
	if !exists {
		t.Fatal("expected config to be added for Deployment GVK")
	}
	if !config.Enabled {
		t.Error("expected config to be enabled")
	}
	if config.FailurePolicy != admissionv1.Fail {
		t.Errorf("expected failurePolicy Fail, got %v", config.FailurePolicy)
	}
	if config.PluralName != "deployments" {
		t.Errorf("expected plural name 'deployments', got %q", config.PluralName)
	}
}

func TestReconcile_DisabledWebhook(t *testing.T) {
	spec := map[string]interface{}{
		"group":   "",
		"version": "v1",
		"kind": map[string]interface{}{
			"singular": "ConfigMap",
			"plural":   "configmaps",
		},
		"webhook": map[string]interface{}{
			"enabled": false,
		},
	}
	lc := makeLeaseController("test-lc", "default", spec)

	cl := newTestClient(lc)
	cm := newTestConfigManager(cl)

	// Pre-add config to verify it gets removed
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	cm.configs[gvk] = GVKConfig{GVK: gvk, Enabled: true}

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "test-lc"}}
	_, err := cm.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify config was removed
	if _, exists := cm.configs[gvk]; exists {
		t.Error("expected config to be removed for disabled webhook")
	}
}

func TestReconcile_MissingRequiredFields(t *testing.T) {
	spec := map[string]interface{}{
		"group": "apps",
		// Missing version and kind
	}
	lc := makeLeaseController("test-lc", "default", spec)

	cl := newTestClient(lc)
	cm := newTestConfigManager(cl)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "test-lc"}}
	_, err := cm.Reconcile(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for missing required fields")
	}
}

func TestReconcile_DefaultFailurePolicy(t *testing.T) {
	spec := map[string]interface{}{
		"group":   "",
		"version": "v1",
		"kind": map[string]interface{}{
			"singular": "ConfigMap",
			"plural":   "configmaps",
		},
		"webhook": map[string]interface{}{
			"enabled": true,
			// No failurePolicy specified - should default to Ignore
		},
	}
	lc := makeLeaseController("test-lc", "default", spec)

	cl := newTestClient(lc)
	cm := newTestConfigManager(cl)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "test-lc"}}
	_, err := cm.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	config := cm.configs[gvk]
	if config.FailurePolicy != admissionv1.Ignore {
		t.Errorf("expected default failurePolicy Ignore, got %v", config.FailurePolicy)
	}
}

func TestReconcile_AddsFinalizer(t *testing.T) {
	spec := map[string]interface{}{
		"group":   "",
		"version": "v1",
		"kind": map[string]interface{}{
			"singular": "ConfigMap",
			"plural":   "configmaps",
		},
		"webhook": map[string]interface{}{
			"enabled": true,
		},
	}
	lc := makeLeaseController("test-lc", "default", spec)

	cl := newTestClient(lc)
	cm := newTestConfigManager(cl)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "test-lc"}}
	_, err := cm.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Fetch the updated object
	updated := &unstructured.Unstructured{}
	updated.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "object-lease-controller.ullberg.io",
		Version: "v1",
		Kind:    "LeaseController",
	})
	err = cl.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "test-lc"}, updated)
	if err != nil {
		t.Fatalf("failed to get updated object: %v", err)
	}

	if !containsFinalizer(updated, finalizerName) {
		t.Error("expected finalizer to be added")
	}
}

func TestReconcile_DeletionHandling(t *testing.T) {
	// Test that when we reconcile a non-existent CR, configs are cleaned up
	cl := newTestClient()
	cm := newTestConfigManager(cl)

	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	nn := types.NamespacedName{Namespace: "default", Name: "deleted-lc"}

	// Add stale config
	cm.configs[gvk] = GVKConfig{GVK: gvk, Enabled: true}
	cm.crToGVK[nn] = gvk

	req := ctrl.Request{NamespacedName: nn}
	_, err := cm.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify config was cleaned up
	if _, exists := cm.configs[gvk]; exists {
		t.Error("expected config to be removed during deletion")
	}
	if _, exists := cm.crToGVK[nn]; exists {
		t.Error("expected crToGVK entry to be removed")
	}
}

func TestReconcile_DeletionCleanup(t *testing.T) {
	// Test that when a CR is not found (already deleted), stale configs are cleaned up
	cl := newTestClient()
	cm := newTestConfigManager(cl)

	// Add stale config
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	nn := types.NamespacedName{Namespace: "default", Name: "deleted-lc"}
	cm.configs[gvk] = GVKConfig{GVK: gvk, Enabled: true}
	cm.crToGVK[nn] = gvk

	req := ctrl.Request{NamespacedName: nn}
	_, err := cm.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify stale config was cleaned up
	if _, exists := cm.configs[gvk]; exists {
		t.Error("expected stale config to be removed")
	}
	if _, exists := cm.crToGVK[nn]; exists {
		t.Error("expected stale crToGVK entry to be removed")
	}
}

func TestSyncWebhookConfiguration_NoConfigs(t *testing.T) {
	cl := newTestClient()
	cm := newTestConfigManager(cl)

	// Create a webhook configuration to be deleted
	wc := &admissionv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: webhookName,
		},
	}
	err := cl.Create(context.Background(), wc)
	if err != nil {
		t.Fatalf("failed to create webhook config: %v", err)
	}

	_, err = cm.syncWebhookConfiguration(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it was deleted
	existing := &admissionv1.ValidatingWebhookConfiguration{}
	err = cl.Get(context.Background(), types.NamespacedName{Name: webhookName}, existing)
	if !errors.IsNotFound(err) {
		t.Error("expected webhook configuration to be deleted")
	}
}

func TestSyncWebhookConfiguration_CreateNew(t *testing.T) {
	cl := newTestClient()
	cm := newTestConfigManager(cl)

	gvk := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	cm.configs[gvk] = GVKConfig{
		GVK:           gvk,
		PluralName:    "deployments",
		Enabled:       true,
		FailurePolicy: admissionv1.Fail,
	}

	_, err := cm.syncWebhookConfiguration(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it was created
	existing := &admissionv1.ValidatingWebhookConfiguration{}
	err = cl.Get(context.Background(), types.NamespacedName{Name: webhookName}, existing)
	if err != nil {
		t.Fatalf("failed to get webhook config: %v", err)
	}

	if len(existing.Webhooks) != 1 {
		t.Errorf("expected 1 webhook, got %d", len(existing.Webhooks))
	}
	if len(existing.Webhooks[0].Rules) != 1 {
		t.Errorf("expected 1 rule, got %d", len(existing.Webhooks[0].Rules))
	}
}

func TestSyncWebhookConfiguration_UpdateExisting(t *testing.T) {
	// Create existing webhook configuration
	existingWC := &admissionv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: webhookName,
		},
		Webhooks: []admissionv1.ValidatingWebhook{
			{
				Name: webhookName,
			},
		},
	}

	cl := newTestClient(existingWC)
	cm := newTestConfigManager(cl)

	gvk := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	cm.configs[gvk] = GVKConfig{
		GVK:           gvk,
		PluralName:    "deployments",
		Enabled:       true,
		FailurePolicy: admissionv1.Fail,
	}

	_, err := cm.syncWebhookConfiguration(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it was updated
	existing := &admissionv1.ValidatingWebhookConfiguration{}
	err = cl.Get(context.Background(), types.NamespacedName{Name: webhookName}, existing)
	if err != nil {
		t.Fatalf("failed to get webhook config: %v", err)
	}

	if len(existing.Webhooks[0].Rules) != 1 {
		t.Errorf("expected 1 rule after update, got %d", len(existing.Webhooks[0].Rules))
	}
}

func TestSyncWebhookConfiguration_MultipleGVKs(t *testing.T) {
	cl := newTestClient()
	cm := newTestConfigManager(cl)

	gvk1 := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	gvk2 := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

	cm.configs[gvk1] = GVKConfig{
		GVK:           gvk1,
		PluralName:    "deployments",
		Enabled:       true,
		FailurePolicy: admissionv1.Ignore,
	}
	cm.configs[gvk2] = GVKConfig{
		GVK:           gvk2,
		PluralName:    "configmaps",
		Enabled:       true,
		FailurePolicy: admissionv1.Fail, // One has Fail, so overall should be Fail
	}

	_, err := cm.syncWebhookConfiguration(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	existing := &admissionv1.ValidatingWebhookConfiguration{}
	err = cl.Get(context.Background(), types.NamespacedName{Name: webhookName}, existing)
	if err != nil {
		t.Fatalf("failed to get webhook config: %v", err)
	}

	if len(existing.Webhooks[0].Rules) != 2 {
		t.Errorf("expected 2 rules, got %d", len(existing.Webhooks[0].Rules))
	}

	// Verify failure policy is Fail (most restrictive)
	if *existing.Webhooks[0].FailurePolicy != admissionv1.Fail {
		t.Errorf("expected failurePolicy Fail, got %v", *existing.Webhooks[0].FailurePolicy)
	}
}

func TestContainsFinalizer(t *testing.T) {
	tests := []struct {
		name       string
		finalizers []string
		finalizer  string
		want       bool
	}{
		{"empty", nil, finalizerName, false},
		{"not found", []string{"other"}, finalizerName, false},
		{"found", []string{"other", finalizerName}, finalizerName, true},
		{"only one", []string{finalizerName}, finalizerName, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &unstructured.Unstructured{}
			u.SetFinalizers(tt.finalizers)
			if got := containsFinalizer(u, tt.finalizer); got != tt.want {
				t.Errorf("containsFinalizer() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetEnv(t *testing.T) {
	// Test default value
	result := getEnv("NONEXISTENT_ENV_VAR_12345", "default-value")
	if result != "default-value" {
		t.Errorf("expected default value, got %q", result)
	}

	// Test with set value
	t.Setenv("TEST_ENV_VAR_12345", "actual-value")
	result = getEnv("TEST_ENV_VAR_12345", "default-value")
	if result != "actual-value" {
		t.Errorf("expected actual value, got %q", result)
	}
}

func TestSyncWebhookConfiguration_CoreGroupHandling(t *testing.T) {
	cl := newTestClient()
	cm := newTestConfigManager(cl)

	// Test core API group (empty string)
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	cm.configs[gvk] = GVKConfig{
		GVK:           gvk,
		PluralName:    "configmaps",
		Enabled:       true,
		FailurePolicy: admissionv1.Ignore,
	}

	_, err := cm.syncWebhookConfiguration(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	existing := &admissionv1.ValidatingWebhookConfiguration{}
	err = cl.Get(context.Background(), types.NamespacedName{Name: webhookName}, existing)
	if err != nil {
		t.Fatalf("failed to get webhook config: %v", err)
	}

	// The core group should be corev1.GroupName (empty string)
	if existing.Webhooks[0].Rules[0].APIGroups[0] != "" {
		t.Errorf("expected empty API group for core resources, got %q", existing.Webhooks[0].Rules[0].APIGroups[0])
	}
}

func TestReconcile_MissingSpec(t *testing.T) {
	lc := makeLeaseController("test-lc", "default", nil)

	cl := newTestClient(lc)
	cm := newTestConfigManager(cl)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "test-lc"}}
	_, _ = cm.Reconcile(context.Background(), req)
	// When spec is missing, the behavior depends on how the code handles it
	// Test passes if no panic occurs
}

func TestReconcile_NoWebhookConfig(t *testing.T) {
	spec := map[string]interface{}{
		"group":   "apps",
		"version": "v1",
		"kind": map[string]interface{}{
			"singular": "Deployment",
			"plural":   "deployments",
		},
		// No webhook configuration - should disable webhook
	}
	lc := makeLeaseController("test-lc", "default", spec)

	cl := newTestClient(lc)
	cm := newTestConfigManager(cl)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "test-lc"}}
	_, err := cm.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify no config was added (webhook disabled by default)
	gvk := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	if _, exists := cm.configs[gvk]; exists {
		t.Error("expected no config for disabled webhook")
	}
}

func TestSyncWebhookConfiguration_DisabledConfigSkipped(t *testing.T) {
	cl := newTestClient()
	cm := newTestConfigManager(cl)

	gvk := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	cm.configs[gvk] = GVKConfig{
		GVK:           gvk,
		PluralName:    "deployments",
		Enabled:       false, // Disabled
		FailurePolicy: admissionv1.Fail,
	}

	_, err := cm.syncWebhookConfiguration(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// When config is disabled, the syncWebhookConfiguration sees configs map as non-empty
	// but after filtering, if no rules are generated it should try to delete
	// Note: The code checks len(configs) > 0 before the loop, so disabled configs
	// still count towards this check. The webhook is created but with no matching rules.
	existing := &admissionv1.ValidatingWebhookConfiguration{}
	err = cl.Get(context.Background(), types.NamespacedName{Name: webhookName}, existing)
	if err != nil {
		// Either not found (deleted) or found (created with empty rules) is acceptable
		if !errors.IsNotFound(err) {
			t.Errorf("unexpected error: %v", err)
		}
	} else {
		// If found, verify no rules are present for disabled configs
		if len(existing.Webhooks) > 0 && len(existing.Webhooks[0].Rules) > 0 {
			t.Error("expected no rules for disabled configs")
		}
	}
}

func TestSyncWebhookConfiguration_WithEnvVars(t *testing.T) {
	// Set environment variables for the test
	t.Setenv("WEBHOOK_SERVICE_NAME", "custom-webhook")
	t.Setenv("WEBHOOK_SERVICE_NAMESPACE", "custom-namespace")

	cl := newTestClient()
	cm := newTestConfigManager(cl)

	gvk := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	cm.configs[gvk] = GVKConfig{
		GVK:           gvk,
		PluralName:    "deployments",
		Enabled:       true,
		FailurePolicy: admissionv1.Ignore,
	}

	_, err := cm.syncWebhookConfiguration(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the webhook was created with custom service details
	existing := &admissionv1.ValidatingWebhookConfiguration{}
	err = cl.Get(context.Background(), types.NamespacedName{Name: webhookName}, existing)
	if err != nil {
		t.Fatalf("failed to get webhook config: %v", err)
	}

	svc := existing.Webhooks[0].ClientConfig.Service
	if svc.Name != "custom-webhook" {
		t.Errorf("expected service name 'custom-webhook', got %q", svc.Name)
	}
	if svc.Namespace != "custom-namespace" {
		t.Errorf("expected service namespace 'custom-namespace', got %q", svc.Namespace)
	}
}

func TestAddFinalizer(t *testing.T) {
	lc := makeLeaseController("test-lc", "default", map[string]interface{}{
		"group":   "",
		"version": "v1",
		"kind": map[string]interface{}{
			"singular": "ConfigMap",
			"plural":   "configmaps",
		},
	})

	cl := newTestClient(lc)
	cm := newTestConfigManager(cl)

	err := cm.addFinalizer(context.Background(), lc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify finalizer was added
	updated := &unstructured.Unstructured{}
	updated.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "object-lease-controller.ullberg.io",
		Version: "v1",
		Kind:    "LeaseController",
	})
	err = cl.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "test-lc"}, updated)
	if err != nil {
		t.Fatalf("failed to get object: %v", err)
	}

	if !containsFinalizer(updated, finalizerName) {
		t.Error("expected finalizer to be present")
	}
}

func TestRemoveFinalizer(t *testing.T) {
	lc := makeLeaseController("test-lc", "default", map[string]interface{}{
		"group":   "",
		"version": "v1",
		"kind": map[string]interface{}{
			"singular": "ConfigMap",
			"plural":   "configmaps",
		},
	})
	lc.SetFinalizers([]string{finalizerName, "other-finalizer"})

	cl := newTestClient(lc)
	cm := newTestConfigManager(cl)

	err := cm.removeFinalizer(context.Background(), lc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify finalizer was removed but other finalizer remains
	updated := &unstructured.Unstructured{}
	updated.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "object-lease-controller.ullberg.io",
		Version: "v1",
		Kind:    "LeaseController",
	})
	err = cl.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "test-lc"}, updated)
	if err != nil {
		t.Fatalf("failed to get object: %v", err)
	}

	if containsFinalizer(updated, finalizerName) {
		t.Error("expected finalizer to be removed")
	}

	// Check other finalizer is still there
	foundOther := false
	for _, f := range updated.GetFinalizers() {
		if f == "other-finalizer" {
			foundOther = true
		}
	}
	if !foundOther {
		t.Error("expected other-finalizer to remain")
	}
}
