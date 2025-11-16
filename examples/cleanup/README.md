# Cleanup Job Examples

This directory contains example configurations for using cleanup jobs with the object-lease-controller.

## Examples

1. **backup-to-s3.yaml** - Complete example of backing up an object to S3 before deletion
2. **notify-webhook.yaml** - Example of notifying an external webhook when an object expires
3. **cleanup-related-resources.yaml** - Example of cleaning up related Kubernetes resources

## Quick Start

1. Create a ConfigMap with your cleanup script
2. Create a ServiceAccount with necessary permissions
3. Annotate your resource with cleanup job configuration
4. When the lease expires, the cleanup job runs automatically

## Annotations

| Annotation | Required | Default | Description |
|------------|----------|---------|-------------|
| `object-lease-controller.ullberg.io/on-delete-job` | Yes (only if cleanup is needed) | - | ConfigMap reference in format `configmap-name/script-key` |
| `object-lease-controller.ullberg.io/job-service-account` | No | `default` | ServiceAccount to run the Job as |
| `object-lease-controller.ullberg.io/job-image` | No | `bitnami/kubectl:latest` | Container image for running the script |
| `object-lease-controller.ullberg.io/job-env-secrets` | No | - | Comma-separated list of Secret names to mount as environment variables |
| `object-lease-controller.ullberg.io/job-wait` | No | `false` | Wait for Job completion before deleting object |
| `object-lease-controller.ullberg.io/job-timeout` | No | `5m` | Maximum time to wait for Job completion |
| `object-lease-controller.ullberg.io/job-ttl` | No | `300` | TTL in seconds for Job cleanup |
| `object-lease-controller.ullberg.io/job-backoff-limit` | No | `3` | Number of retries for failed Jobs |

## Environment Variables

The cleanup script receives these environment variables:

- `OBJECT_NAME` - Name of the object being deleted
- `OBJECT_NAMESPACE` - Namespace of the object
- `OBJECT_KIND` - Kind (e.g., "Application")
- `OBJECT_GROUP` - API group
- `OBJECT_VERSION` - API version
- `OBJECT_UID` - UID of the object
- `OBJECT_RESOURCE_VERSION` - Resource version
- `LEASE_STARTED_AT` - RFC3339 timestamp when lease started
- `LEASE_EXPIRED_AT` - RFC3339 timestamp when lease expired
- `OBJECT_LABELS` - JSON-encoded labels
- `OBJECT_ANNOTATIONS` - JSON-encoded annotations
