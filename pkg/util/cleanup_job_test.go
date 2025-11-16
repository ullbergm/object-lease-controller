package util

import (
	"context"
	"fmt"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestParseCleanupJobConfig_NoConfig(t *testing.T) {
	annotations := map[string]string{}
	annotationKeys := map[string]string{
		"OnDeleteJob": "on-delete-job",
	}

	config, err := ParseCleanupJobConfig(annotations, annotationKeys)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if config != nil {
		t.Errorf("Expected nil config, got %v", config)
	}
}

func TestParseCleanupJobConfig_ValidConfig(t *testing.T) {
	annotations := map[string]string{
		"on-delete-job":       "my-scripts/backup.sh",
		"job-service-account": "backup-sa",
		"job-image":           "custom/image:v1",
		"job-wait":            "true",
		"job-timeout":         "10m",
		"job-ttl":             "600",
		"job-backoff-limit":   "5",
	}
	annotationKeys := map[string]string{
		"OnDeleteJob":       "on-delete-job",
		"JobServiceAccount": "job-service-account",
		"JobImage":          "job-image",
		"JobWait":           "job-wait",
		"JobTimeout":        "job-timeout",
		"JobTTL":            "job-ttl",
		"JobBackoffLimit":   "job-backoff-limit",
	}

	config, err := ParseCleanupJobConfig(annotations, annotationKeys)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if config == nil {
		t.Fatal("Expected config, got nil")
	}

	if config.ConfigMapName != "my-scripts" {
		t.Errorf("Expected ConfigMapName 'my-scripts', got %s", config.ConfigMapName)
	}
	if config.ScriptKey != "backup.sh" {
		t.Errorf("Expected ScriptKey 'backup.sh', got %s", config.ScriptKey)
	}
	if config.ServiceAccount != "backup-sa" {
		t.Errorf("Expected ServiceAccount 'backup-sa', got %s", config.ServiceAccount)
	}
	if config.Image != "custom/image:v1" {
		t.Errorf("Expected Image 'custom/image:v1', got %s", config.Image)
	}
	if !config.Wait {
		t.Errorf("Expected Wait true, got false")
	}
	if config.Timeout != 10*time.Minute {
		t.Errorf("Expected Timeout 10m, got %v", config.Timeout)
	}
	if config.TTLSecondsAfterFinished != 600 {
		t.Errorf("Expected TTLSecondsAfterFinished 600, got %d", config.TTLSecondsAfterFinished)
	}
	if config.BackoffLimit != 5 {
		t.Errorf("Expected BackoffLimit 5, got %d", config.BackoffLimit)
	}
}

func TestParseCleanupJobConfig_InvalidFormat(t *testing.T) {
	annotations := map[string]string{
		"on-delete-job": "invalid-format-no-slash",
	}
	annotationKeys := map[string]string{
		"OnDeleteJob": "on-delete-job",
	}

	config, err := ParseCleanupJobConfig(annotations, annotationKeys)
	if err == nil {
		t.Errorf("Expected error for invalid format, got nil")
	}
	if config != nil {
		t.Errorf("Expected nil config, got %v", config)
	}
}

func TestParseCleanupJobConfig_InvalidWait(t *testing.T) {
	annotations := map[string]string{
		"on-delete-job": "scripts/cleanup.sh",
		"job-wait":      "notabool",
	}
	annotationKeys := map[string]string{
		"OnDeleteJob": "on-delete-job",
		"JobWait":     "job-wait",
	}

	cfg, err := ParseCleanupJobConfig(annotations, annotationKeys)
	if err == nil {
		t.Fatalf("expected error for invalid job-wait, got nil, config=%v", cfg)
	}
}

func TestParseCleanupJobConfig_InvalidTimeout(t *testing.T) {
	annotations := map[string]string{
		"on-delete-job": "scripts/cleanup.sh",
		"job-timeout":   "not-a-duration",
	}
	annotationKeys := map[string]string{
		"OnDeleteJob": "on-delete-job",
		"JobTimeout":  "job-timeout",
	}

	cfg, err := ParseCleanupJobConfig(annotations, annotationKeys)
	if err == nil {
		t.Fatalf("expected error for invalid job-timeout, got nil, config=%v", cfg)
	}
}

func TestParseCleanupJobConfig_InvalidTTLAndBackoff(t *testing.T) {
	annotations := map[string]string{
		"on-delete-job":     "scripts/cleanup.sh",
		"job-ttl":           "notAnInt",
		"job-backoff-limit": "notAnInt",
	}
	annotationKeys := map[string]string{
		"OnDeleteJob":     "on-delete-job",
		"JobTTL":          "job-ttl",
		"JobBackoffLimit": "job-backoff-limit",
	}

	// TTL parse error
	cfg, err := ParseCleanupJobConfig(annotations, annotationKeys)
	if err == nil {
		t.Fatalf("expected error for invalid job-ttl, got nil, config=%v", cfg)
	}

	// backoff limit error
	// switch TTL to a valid integer so we hit the backoff parse code path
	annotations["job-ttl"] = "120"
	cfg, err = ParseCleanupJobConfig(annotations, annotationKeys)
	if err == nil {
		t.Fatalf("expected error for invalid job-backoff-limit, got nil, config=%v", cfg)
	}
}

func TestParseCleanupJobConfig_Defaults(t *testing.T) {
	annotations := map[string]string{
		"on-delete-job": "scripts/cleanup.sh",
	}
	annotationKeys := map[string]string{
		"OnDeleteJob":       "on-delete-job",
		"JobServiceAccount": "job-service-account",
		"JobImage":          "job-image",
		"JobWait":           "job-wait",
		"JobTimeout":        "job-timeout",
		"JobTTL":            "job-ttl",
		"JobBackoffLimit":   "job-backoff-limit",
	}

	config, err := ParseCleanupJobConfig(annotations, annotationKeys)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if config == nil {
		t.Fatal("Expected config, got nil")
	}

	if config.ServiceAccount != DefaultServiceAccount {
		t.Errorf("Expected default ServiceAccount '%s', got %s", DefaultServiceAccount, config.ServiceAccount)
	}
	if config.Image != DefaultJobImage {
		t.Errorf("Expected default Image '%s', got %s", DefaultJobImage, config.Image)
	}
	if config.Wait {
		t.Errorf("Expected Wait false, got true")
	}
	if config.TTLSecondsAfterFinished != DefaultJobTTL {
		t.Errorf("Expected default TTL %d, got %d", DefaultJobTTL, config.TTLSecondsAfterFinished)
	}
	if config.BackoffLimit != DefaultJobBackoffLimit {
		t.Errorf("Expected default BackoffLimit %d, got %d", DefaultJobBackoffLimit, config.BackoffLimit)
	}
}

func TestCreateCleanupJob(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = batchv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	client := fake.NewClientBuilder().WithScheme(scheme).Build()

	obj := &unstructured.Unstructured{}
	obj.SetName("test-obj")
	obj.SetNamespace("test-ns")
	obj.SetUID("test-uid")
	obj.SetResourceVersion("123")
	obj.SetLabels(map[string]string{"app": "test"})
	obj.SetAnnotations(map[string]string{"key": "value"})

	gvk := schema.GroupVersionKind{
		Group:   "example.com",
		Version: "v1",
		Kind:    "TestKind",
	}

	config := &CleanupJobConfig{
		ConfigMapName:           "my-scripts",
		ScriptKey:               "cleanup.sh",
		ServiceAccount:          "test-sa",
		Image:                   "test/image:v1",
		Wait:                    false,
		Timeout:                 5 * time.Minute,
		TTLSecondsAfterFinished: 300,
		BackoffLimit:            3,
	}

	leaseStart := time.Now().Add(-1 * time.Hour)
	leaseExpire := time.Now()

	job, err := CreateCleanupJob(context.Background(), client, obj, gvk, config, leaseStart, leaseExpire)
	if err != nil {
		t.Fatalf("Failed to create cleanup job: %v", err)
	}

	if job.Namespace != "test-ns" {
		t.Errorf("Expected namespace 'test-ns', got %s", job.Namespace)
	}

	if job.Spec.Template.Spec.ServiceAccountName != "test-sa" {
		t.Errorf("Expected service account 'test-sa', got %s", job.Spec.Template.Spec.ServiceAccountName)
	}

	if len(job.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("Expected 1 container, got %d", len(job.Spec.Template.Spec.Containers))
	}

	container := job.Spec.Template.Spec.Containers[0]
	if container.Image != "test/image:v1" {
		t.Errorf("Expected image 'test/image:v1', got %s", container.Image)
	}

	// Check environment variables
	envMap := make(map[string]string)
	for _, env := range container.Env {
		envMap[env.Name] = env.Value
	}

	if envMap["OBJECT_NAME"] != "test-obj" {
		t.Errorf("Expected OBJECT_NAME 'test-obj', got %s", envMap["OBJECT_NAME"])
	}
	if envMap["OBJECT_NAMESPACE"] != "test-ns" {
		t.Errorf("Expected OBJECT_NAMESPACE 'test-ns', got %s", envMap["OBJECT_NAMESPACE"])
	}
	if envMap["OBJECT_KIND"] != "TestKind" {
		t.Errorf("Expected OBJECT_KIND 'TestKind', got %s", envMap["OBJECT_KIND"])
	}
	if envMap["OBJECT_GROUP"] != "example.com" {
		t.Errorf("Expected OBJECT_GROUP 'example.com', got %s", envMap["OBJECT_GROUP"])
	}
	if envMap["OBJECT_VERSION"] != "v1" {
		t.Errorf("Expected OBJECT_VERSION 'v1', got %s", envMap["OBJECT_VERSION"])
	}
}

// failCreateClient returns an error for Create() to simulate job creation failure
type failCreateClient struct{ client.Client }

func (c *failCreateClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	return fmt.Errorf("create error")
}

func TestCreateCleanupJob_FailsOnClientCreate(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = batchv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	base := fake.NewClientBuilder().WithScheme(scheme).Build()
	fc := &failCreateClient{Client: base}

	obj := &unstructured.Unstructured{}
	obj.SetName("fail-obj")
	obj.SetNamespace("default")

	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

	cfg := &CleanupJobConfig{
		ConfigMapName:           "cm",
		ScriptKey:               "script",
		ServiceAccount:          DefaultServiceAccount,
		Image:                   DefaultJobImage,
		TTLSecondsAfterFinished: DefaultJobTTL,
		BackoffLimit:            DefaultJobBackoffLimit,
	}

	// Should return error because Create fails
	if _, err := CreateCleanupJob(context.Background(), fc, obj, gvk, cfg, time.Now(), time.Now()); err == nil {
		t.Fatalf("expected CreateCleanupJob to return error on client.Create failure")
	}
}

func TestCreateCleanupJob_LabelsMarshalError(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = batchv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	client := fake.NewClientBuilder().WithScheme(scheme).Build()

	obj := &unstructured.Unstructured{}
	obj.SetName("test-obj")
	obj.SetNamespace("test-ns")

	gvk := schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "TestKind"}

	cfg := &CleanupJobConfig{ConfigMapName: "cm", ScriptKey: "script"}

	// Override jsonMarshal to simulate JSON errors
	old := jsonMarshal
	jsonMarshal = func(v interface{}) ([]byte, error) { return nil, fmt.Errorf("json error") }
	defer func() { jsonMarshal = old }()

	if _, err := CreateCleanupJob(context.Background(), client, obj, gvk, cfg, time.Now(), time.Now()); err == nil {
		t.Fatalf("expected error creating job when labels JSON marshal fails")
	}
}

func TestCreateCleanupJob_AnnotationsMarshalError(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = batchv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	client := fake.NewClientBuilder().WithScheme(scheme).Build()

	obj := &unstructured.Unstructured{}
	obj.SetName("test-obj")
	obj.SetNamespace("test-ns")

	gvk := schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "TestKind"}

	cfg := &CleanupJobConfig{ConfigMapName: "cm", ScriptKey: "script"}

	// jsonMarshal should succeed for labels (first call) then fail for annotations (second call)
	old := jsonMarshal
	calls := 0
	jsonMarshal = func(v interface{}) ([]byte, error) {
		calls++
		if calls == 1 {
			return old(v)
		}
		return nil, fmt.Errorf("json annotations error")
	}
	defer func() { jsonMarshal = old }()

	if _, err := CreateCleanupJob(context.Background(), client, obj, gvk, cfg, time.Now(), time.Now()); err == nil {
		t.Fatalf("expected error creating job when annotations JSON marshal fails")
	}
}

func TestWaitForJobCompletion_Success(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = batchv1.AddToScheme(scheme)

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job",
			Namespace: "test-ns",
		},
		Status: batchv1.JobStatus{
			Conditions: []batchv1.JobCondition{
				{
					Type:   batchv1.JobComplete,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(job).Build()

	err := WaitForJobCompletion(context.Background(), client, job, 10*time.Second)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestWaitForJobCompletion_Timeout(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = batchv1.AddToScheme(scheme)

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job",
			Namespace: "test-ns",
		},
		Status: batchv1.JobStatus{
			Conditions: []batchv1.JobCondition{},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(job).Build()

	err := WaitForJobCompletion(context.Background(), client, job, 1*time.Second)
	if err == nil {
		t.Errorf("Expected timeout error, got nil")
	}
}

func TestWaitForJobCompletion_Failed(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = batchv1.AddToScheme(scheme)

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job-failed",
			Namespace: "test-ns",
		},
		Status: batchv1.JobStatus{
			Conditions: []batchv1.JobCondition{
				{
					Type:    batchv1.JobFailed,
					Status:  corev1.ConditionTrue,
					Message: "failed by test",
				},
			},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(job).Build()

	err := WaitForJobCompletion(context.Background(), client, job, 10*time.Second)
	if err == nil {
		t.Errorf("Expected job failed error, got nil")
	}
}

// failGetClient returns error from Get()
type failGetClient struct{ client.Client }

func (f *failGetClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	return fmt.Errorf("get error")
}

func TestWaitForJobCompletion_GetError(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = batchv1.AddToScheme(scheme)

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "test-job", Namespace: "ns"},
	}

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(job).Build()
	fc := &failGetClient{Client: base}

	err := WaitForJobCompletion(context.Background(), fc, job, 10*time.Second)
	if err == nil {
		t.Fatalf("expected error when Get fails, got nil")
	}
}
