package webhook

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/go-logr/logr"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	webhookName   = "lease-validator.object-lease-controller.ullberg.io"
	finalizerName = "webhook.object-lease-controller.ullberg.io/finalizer"
)

// GVKConfig holds configuration for a specific GVK
type GVKConfig struct {
	GVK           schema.GroupVersionKind
	PluralName    string
	Enabled       bool
	FailurePolicy admissionv1.FailurePolicyType
}

// ConfigManager manages webhook configuration based on LeaseController CRs
type ConfigManager struct {
	client        client.Client
	ttlAnnotation string
	log           logr.Logger

	mu      sync.RWMutex
	configs map[schema.GroupVersionKind]GVKConfig
	// Track which LeaseController manages which GVK for cleanup
	crToGVK map[types.NamespacedName]schema.GroupVersionKind
}

// NewConfigManager creates a new configuration manager
func NewConfigManager(c client.Client, ttlAnnotation string, log logr.Logger) *ConfigManager {
	return &ConfigManager{
		client:        c,
		ttlAnnotation: ttlAnnotation,
		log:           log,
		configs:       make(map[schema.GroupVersionKind]GVKConfig),
		crToGVK:       make(map[types.NamespacedName]schema.GroupVersionKind),
	}
}

// ShouldValidate checks if a GVK should be validated
func (cm *ConfigManager) ShouldValidate(gvk schema.GroupVersionKind) bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	config, exists := cm.configs[gvk]
	return exists && config.Enabled
}

// SetupWithManager sets up the controller with the manager
func (cm *ConfigManager) SetupWithManager(mgr ctrl.Manager) error {
	// Register the LeaseController CRD scheme
	leaseControllerGVK := schema.GroupVersionKind{
		Group:   "object-lease-controller.ullberg.io",
		Version: "v1",
		Kind:    "LeaseController",
	}

	// Create an unstructured object for watching
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(leaseControllerGVK)

	// Build controller
	return ctrl.NewControllerManagedBy(mgr).
		Named("webhook-config-manager").
		For(u).
		WithEventFilter(cm.watchPredicates()).
		Complete(cm)
}

func (cm *ConfigManager) watchPredicates() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return true
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return true
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return true
		},
	}
}

// Reconcile handles LeaseController CR changes
func (cm *ConfigManager) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := cm.log.WithValues("leasecontroller", req.NamespacedName)

	// Fetch the LeaseController CR
	leaseController := &unstructured.Unstructured{}
	leaseController.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "object-lease-controller.ullberg.io",
		Version: "v1",
		Kind:    "LeaseController",
	})

	err := cm.client.Get(ctx, req.NamespacedName, leaseController)
	if err != nil {
		if errors.IsNotFound(err) {
			// CR was deleted and finalizer already removed
			// Clean up any stale configuration just in case
			log.Info("LeaseController not found, cleaning up any stale configuration")

			cm.mu.Lock()
			gvk, exists := cm.crToGVK[req.NamespacedName]
			if exists {
				delete(cm.configs, gvk)
				delete(cm.crToGVK, req.NamespacedName)
				log.Info("removed stale GVK from webhook configuration", "gvk", gvk.String())
			}
			cm.mu.Unlock()

			return cm.syncWebhookConfiguration(ctx)
		}
		return ctrl.Result{}, err
	}

	// Check if the CR is being deleted
	if !leaseController.GetDeletionTimestamp().IsZero() {
		// CR is being deleted
		if containsFinalizer(leaseController, finalizerName) {
			log.Info("LeaseController is being deleted, cleaning up webhook configuration")

			// Look up which GVK this CR was managing
			cm.mu.Lock()
			gvk, exists := cm.crToGVK[req.NamespacedName]
			if exists {
				delete(cm.configs, gvk)
				delete(cm.crToGVK, req.NamespacedName)
				log.Info("removed GVK from webhook configuration due to deletion", "gvk", gvk.String())
			}
			cm.mu.Unlock()

			// Sync webhook configuration
			if _, err := cm.syncWebhookConfiguration(ctx); err != nil {
				log.Error(err, "failed to sync webhook configuration during deletion")
				return ctrl.Result{}, err
			}

			// Remove finalizer
			if err := cm.removeFinalizer(ctx, leaseController); err != nil {
				log.Error(err, "failed to remove finalizer")
				return ctrl.Result{}, err
			}

			log.Info("successfully cleaned up webhook configuration and removed finalizer")
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer if not present
	if !containsFinalizer(leaseController, finalizerName) {
		if err := cm.addFinalizer(ctx, leaseController); err != nil {
			log.Error(err, "failed to add finalizer")
			return ctrl.Result{}, err
		}
		log.Info("added finalizer to LeaseController")
	}

	// Parse the spec
	spec, found, err := unstructured.NestedMap(leaseController.Object, "spec")
	if err != nil || !found {
		log.Error(err, "failed to get spec from LeaseController")
		return ctrl.Result{}, err
	}

	// Extract GVK from spec
	group, _, _ := unstructured.NestedString(spec, "group")
	version, _, _ := unstructured.NestedString(spec, "version")
	kindObj, _, _ := unstructured.NestedMap(spec, "kind")
	kindSingular, _, _ := unstructured.NestedString(kindObj, "singular")
	kindPlural, _, _ := unstructured.NestedString(kindObj, "plural")

	if version == "" || kindSingular == "" || kindPlural == "" {
		log.Info("LeaseController missing required fields", "version", version, "kind", kindSingular, "plural", kindPlural)
		return ctrl.Result{}, fmt.Errorf("missing required fields")
	}

	targetGVK := schema.GroupVersionKind{
		Group:   group,
		Version: version,
		Kind:    kindSingular,
	}

	// Extract webhook configuration
	webhookConfig, _, _ := unstructured.NestedMap(spec, "webhook")
	enabled, _, _ := unstructured.NestedBool(webhookConfig, "enabled")
	failurePolicy, _, _ := unstructured.NestedString(webhookConfig, "failurePolicy")

	// Default to Ignore if not specified
	policy := admissionv1.Ignore
	if failurePolicy == "Fail" {
		policy = admissionv1.Fail
	}

	// Update configuration
	cm.mu.Lock()
	if enabled {
		cm.configs[targetGVK] = GVKConfig{
			GVK:           targetGVK,
			PluralName:    kindPlural,
			Enabled:       true,
			FailurePolicy: policy,
		}
		cm.crToGVK[req.NamespacedName] = targetGVK
		log.Info("enabled webhook for GVK", "gvk", targetGVK.String(), "failurePolicy", policy)
	} else {
		delete(cm.configs, targetGVK)
		delete(cm.crToGVK, req.NamespacedName)
		log.Info("disabled webhook for GVK", "gvk", targetGVK.String())
	}
	cm.mu.Unlock()

	// Sync the ValidatingWebhookConfiguration
	return cm.syncWebhookConfiguration(ctx)
}

// syncWebhookConfiguration updates the ValidatingWebhookConfiguration
func (cm *ConfigManager) syncWebhookConfiguration(ctx context.Context) (ctrl.Result, error) {
	cm.mu.RLock()
	configs := make(map[schema.GroupVersionKind]GVKConfig, len(cm.configs))
	for k, v := range cm.configs {
		configs[k] = v
	}
	cm.mu.RUnlock()

	// If no configs, delete the webhook configuration
	if len(configs) == 0 {
		cm.log.Info("no enabled webhooks, deleting ValidatingWebhookConfiguration")
		webhookConfig := &admissionv1.ValidatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Name: webhookName,
			},
		}
		err := cm.client.Delete(ctx, webhookConfig)
		if err != nil && !errors.IsNotFound(err) {
			cm.log.Error(err, "failed to delete ValidatingWebhookConfiguration")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Build webhook rules
	rules := make([]admissionv1.RuleWithOperations, 0, len(configs))
	failurePolicy := admissionv1.Ignore // Default

	for _, config := range configs {
		if !config.Enabled {
			continue
		}

		// Use the most restrictive failure policy if any GVK has Fail
		if config.FailurePolicy == admissionv1.Fail {
			failurePolicy = admissionv1.Fail
		}

		apiGroup := config.GVK.Group
		if apiGroup == "" {
			apiGroup = corev1.GroupName
		}

		rule := admissionv1.RuleWithOperations{
			Operations: []admissionv1.OperationType{
				admissionv1.Create,
				admissionv1.Update,
			},
			Rule: admissionv1.Rule{
				APIGroups:   []string{apiGroup},
				APIVersions: []string{config.GVK.Version},
				Resources:   []string{config.PluralName},
			},
		}
		rules = append(rules, rule)
	}

	// Get the webhook service details from environment or defaults
	webhookServiceName := getEnv("WEBHOOK_SERVICE_NAME", "lease-webhook-service")
	webhookServiceNamespace := getEnv("WEBHOOK_SERVICE_NAMESPACE", "object-lease-controller-system")
	webhookServicePath := "/validate"

	sideEffects := admissionv1.SideEffectClassNone
	port := int32(443)

	// Create or update the ValidatingWebhookConfiguration
	webhookConfig := &admissionv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: webhookName,
		},
		Webhooks: []admissionv1.ValidatingWebhook{
			{
				Name:                    webhookName,
				FailurePolicy:           &failurePolicy,
				SideEffects:             &sideEffects,
				AdmissionReviewVersions: []string{"v1", "v1beta1"},
				ClientConfig: admissionv1.WebhookClientConfig{
					Service: &admissionv1.ServiceReference{
						Name:      webhookServiceName,
						Namespace: webhookServiceNamespace,
						Path:      &webhookServicePath,
						Port:      &port,
					},
				},
				Rules: rules,
			},
		},
	}

	// Try to get existing configuration
	existing := &admissionv1.ValidatingWebhookConfiguration{}
	err := cm.client.Get(ctx, types.NamespacedName{Name: webhookName}, existing)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new
			cm.log.Info("creating ValidatingWebhookConfiguration", "rules", len(rules))
			return ctrl.Result{}, cm.client.Create(ctx, webhookConfig)
		}
		return ctrl.Result{}, err
	}

	// Update existing
	existing.Webhooks = webhookConfig.Webhooks
	cm.log.Info("updating ValidatingWebhookConfiguration", "rules", len(rules))
	return ctrl.Result{}, cm.client.Update(ctx, existing)
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// containsFinalizer checks if a finalizer is present
func containsFinalizer(obj *unstructured.Unstructured, finalizer string) bool {
	finalizers := obj.GetFinalizers()
	for _, f := range finalizers {
		if f == finalizer {
			return true
		}
	}
	return false
}

// addFinalizer adds a finalizer to the object
func (cm *ConfigManager) addFinalizer(ctx context.Context, obj *unstructured.Unstructured) error {
	finalizers := obj.GetFinalizers()
	finalizers = append(finalizers, finalizerName)
	obj.SetFinalizers(finalizers)
	return cm.client.Update(ctx, obj)
}

// removeFinalizer removes a finalizer from the object
func (cm *ConfigManager) removeFinalizer(ctx context.Context, obj *unstructured.Unstructured) error {
	finalizers := obj.GetFinalizers()
	var newFinalizers []string
	for _, f := range finalizers {
		if f != finalizerName {
			newFinalizers = append(newFinalizers, f)
		}
	}
	obj.SetFinalizers(newFinalizers)
	return cm.client.Update(ctx, obj)
}
