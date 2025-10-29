package util

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	DefaultJobImage        = "bitnami/kubectl:latest"
	DefaultServiceAccount  = "default"
	DefaultJobTTL          = 300
	DefaultJobBackoffLimit = 3
	DefaultJobTimeout      = "5m"
)

// CleanupJobConfig holds the configuration for a cleanup job
type CleanupJobConfig struct {
	ConfigMapName           string
	ScriptKey               string
	ServiceAccount          string
	Image                   string
	Wait                    bool
	Timeout                 time.Duration
	TTLSecondsAfterFinished int32
	BackoffLimit            int32
}

// ParseCleanupJobConfig extracts cleanup job configuration from object annotations
func ParseCleanupJobConfig(annotations map[string]string, annotationKeys map[string]string) (*CleanupJobConfig, error) {
	// Check if cleanup job is configured
	onDeleteJob := annotations[annotationKeys["OnDeleteJob"]]
	if onDeleteJob == "" {
		return nil, nil // No cleanup job configured
	}

	// Parse configmap-name/script-key format
	parts := strings.SplitN(onDeleteJob, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid on-delete-job format: expected 'configmap-name/script-key', got '%s'", onDeleteJob)
	}

	config := &CleanupJobConfig{
		ConfigMapName:           parts[0],
		ScriptKey:               parts[1],
		ServiceAccount:          DefaultServiceAccount,
		Image:                   DefaultJobImage,
		Wait:                    false,
		Timeout:                 5 * time.Minute,
		TTLSecondsAfterFinished: DefaultJobTTL,
		BackoffLimit:            DefaultJobBackoffLimit,
	}

	// Parse optional service account
	if sa := annotations[annotationKeys["JobServiceAccount"]]; sa != "" {
		config.ServiceAccount = sa
	}

	// Parse optional image
	if img := annotations[annotationKeys["JobImage"]]; img != "" {
		config.Image = img
	}

	// Parse wait flag
	if waitStr := annotations[annotationKeys["JobWait"]]; waitStr != "" {
		wait, err := strconv.ParseBool(waitStr)
		if err != nil {
			return nil, fmt.Errorf("invalid job-wait value: %w", err)
		}
		config.Wait = wait
	}

	// Parse timeout
	if timeoutStr := annotations[annotationKeys["JobTimeout"]]; timeoutStr != "" {
		timeout, err := ParseFlexibleDuration(timeoutStr)
		if err != nil {
			return nil, fmt.Errorf("invalid job-timeout value: %w", err)
		}
		config.Timeout = timeout
	}

	// Parse TTL
	if ttlStr := annotations[annotationKeys["JobTTL"]]; ttlStr != "" {
		ttl, err := strconv.ParseInt(ttlStr, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid job-ttl value: %w", err)
		}
		config.TTLSecondsAfterFinished = int32(ttl)
	}

	// Parse backoff limit
	if backoffStr := annotations[annotationKeys["JobBackoffLimit"]]; backoffStr != "" {
		backoff, err := strconv.ParseInt(backoffStr, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid job-backoff-limit value: %w", err)
		}
		config.BackoffLimit = int32(backoff)
	}

	return config, nil
}

// CreateCleanupJob creates a Kubernetes Job for running a cleanup script
func CreateCleanupJob(
	ctx context.Context,
	c client.Client,
	obj *unstructured.Unstructured,
	gvk schema.GroupVersionKind,
	config *CleanupJobConfig,
	leaseStartedAt, leaseExpiredAt time.Time,
) (*batchv1.Job, error) {
	// Prepare environment variables
	labels, err := json.Marshal(obj.GetLabels())
	if err != nil {
		return nil, fmt.Errorf("failed to marshal object labels to JSON: %w", err)
	}
	annotations, err := json.Marshal(obj.GetAnnotations())
	if err != nil {
		return nil, fmt.Errorf("failed to marshal object annotations to JSON: %w", err)
	}

	envVars := []corev1.EnvVar{
		{Name: "OBJECT_NAME", Value: obj.GetName()},
		{Name: "OBJECT_NAMESPACE", Value: obj.GetNamespace()},
		{Name: "OBJECT_KIND", Value: gvk.Kind},
		{Name: "OBJECT_GROUP", Value: gvk.Group},
		{Name: "OBJECT_VERSION", Value: gvk.Version},
		{Name: "OBJECT_UID", Value: string(obj.GetUID())},
		{Name: "OBJECT_RESOURCE_VERSION", Value: obj.GetResourceVersion()},
		{Name: "LEASE_STARTED_AT", Value: leaseStartedAt.Format(time.RFC3339)},
		{Name: "LEASE_EXPIRED_AT", Value: leaseExpiredAt.Format(time.RFC3339)},
		{Name: "OBJECT_LABELS", Value: string(labels)},
		{Name: "OBJECT_ANNOTATIONS", Value: string(annotations)},
	}

	// Create Job spec
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("lease-cleanup-%s-", obj.GetName()),
			Namespace:    obj.GetNamespace(),
			Labels: map[string]string{
				"object-lease-controller.ullberg.io/source-kind": gvk.Kind,
				"object-lease-controller.ullberg.io/source-name": obj.GetName(),
				"object-lease-controller.ullberg.io/cleanup-job": "true",
			},
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: &config.TTLSecondsAfterFinished,
			BackoffLimit:            &config.BackoffLimit,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyNever,
					ServiceAccountName: config.ServiceAccount,
					Volumes: []corev1.Volume{
						{
							Name: "script",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: config.ConfigMapName,
									},
									DefaultMode: int32Ptr(0755),
									Items: []corev1.KeyToPath{
										{
											Key:  config.ScriptKey,
											Path: "cleanup-script",
										},
									},
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:    "cleanup",
							Image:   config.Image,
							Command: []string{"/scripts/cleanup-script"},
							Env:     envVars,
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "script",
									MountPath: "/scripts",
									ReadOnly:  true,
								},
							},
						},
					},
				},
			},
		},
	}

	// Create the Job
	if err := c.Create(ctx, job); err != nil {
		return nil, fmt.Errorf("failed to create cleanup job: %w", err)
	}

	return job, nil
}

// WaitForJobCompletion waits for a Job to complete with a timeout
func WaitForJobCompletion(ctx context.Context, c client.Client, job *batchv1.Job, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for job completion")
		case <-ticker.C:
			currentJob := &batchv1.Job{}
			if err := c.Get(ctx, client.ObjectKeyFromObject(job), currentJob); err != nil {
				return fmt.Errorf("failed to get job status: %w", err)
			}

			// Check if job completed
			for _, condition := range currentJob.Status.Conditions {
				if condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue {
					return nil // Job completed successfully
				}
				if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
					return fmt.Errorf("job failed: %s", condition.Message)
				}
			}
		}
	}
}

// Helper function to create int32 pointer
func int32Ptr(i int32) *int32 {
	return &i
}
