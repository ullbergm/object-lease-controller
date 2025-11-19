# Object Lease Controller Webhook

## Overview

The Object Lease Controller includes a **validating admission webhook** that validates the TTL annotation format before objects are created or updated in Kubernetes. This prevents invalid lease configurations from being applied.

## Architecture

The webhook system uses a **shared webhook deployment** that serves all LeaseController instances:

- **Single webhook deployment** validates TTL annotations for all configured GVKs
- **Dynamic configuration**: Webhook automatically watches LeaseController CRs and updates ValidatingWebhookConfiguration
- **Per-GVK control**: Each LeaseController CR can enable/disable webhook validation and set failure policy

```
┌─────────────────────┐
│ LeaseController CR  │
│  (Deployments)      │
│  webhook:           │
│    enabled: true    │
│    failurePolicy:   │
│      Fail           │
└──────────┬──────────┘
           │
           ▼
┌─────────────────────────────────┐
│   Webhook ConfigManager         │
│   (watches LeaseController CRs) │
└──────────┬──────────────────────┘
           │
           ▼
┌────────────────────────────────────┐
│  ValidatingWebhookConfiguration    │
│  - rules for Deployments           │
│  - rules for Pods                  │
│  - rules for StatefulSets          │
└────────────────────────────────────┘
```

## Configuration

### LeaseController CR

Enable webhook validation per-GVK in your LeaseController CR:

```yaml
apiVersion: object-lease-controller.ullberg.io/v1
kind: LeaseController
metadata:
  name: leasecontroller-deployments
spec:
  group: "apps"
  version: "v1"
  kind:
    singular: "Deployment"
    plural: "deployments"
  webhook:
    enabled: true            # Enable validation for this GVK
    failurePolicy: Fail      # Reject invalid objects (or "Ignore" to allow)
```

### Webhook Configuration Options

- **`enabled`** (boolean, default: `false`): Enable webhook validation for this GVK
- **`failurePolicy`** (string, default: `"Ignore"`): How to handle webhook failures
  - `Ignore`: Allow object creation/update if webhook fails or is unavailable
  - `Fail`: Reject object creation/update if webhook fails or is unavailable

### Operator-Level Configuration

The webhook deployment is managed by the operator and configured in `config/manager/webhook-deployment.yaml`:

```yaml
spec:
  replicas: 2              # High availability
  resources:
    limits:
      cpu: 500m
      memory: 128Mi
    requests:
      cpu: 10m
      memory: 64Mi
```

## Validation Rules

The webhook validates the `object-lease-controller.ullberg.io/ttl` annotation:

### Valid TTL Formats

- `30s` - seconds
- `5m` - minutes
- `2h` - hours
- `7d` - days
- `1w` - weeks
- `1w2d3h30m` - combined units

### Examples

```yaml
# ✅ Valid - will be accepted
annotations:
  object-lease-controller.ullberg.io/ttl: "2d"

# ✅ Valid - complex duration
annotations:
  object-lease-controller.ullberg.io/ttl: "1w2d3h"

# ❌ Invalid - will be rejected (if failurePolicy: Fail)
annotations:
  object-lease-controller.ullberg.io/ttl: "invalid"

# ❌ Invalid - negative duration
annotations:
  object-lease-controller.ullberg.io/ttl: "-5d"
```

## Certificate Management

The webhook requires TLS certificates. When deployed via OLM or on OpenShift, certificates are automatically managed via the service-ca-operator:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: object-lease-webhook-service
  annotations:
    service.beta.openshift.io/serving-cert-secret-name: object-lease-webhook-cert
spec:
  ports:
  - name: webhook
    port: 443
    targetPort: 9443
```

### OLM/OpenShift Deployment (Recommended)
- **service-ca-operator** automatically creates and rotates certificates
- The annotation `service.beta.openshift.io/serving-cert-secret-name` triggers certificate creation
- Certificates are mounted at `/apiserver.local.config/certificates`
- No manual certificate management needed

### Alternative: cert-manager
For non-OLM deployments, you can use cert-manager:
- Install cert-manager in the cluster
- Create Certificate and Issuer resources
- Mount certificates at `/tmp/k8s-webhook-server/serving-certs`

## Deployment

The webhook is deployed automatically when the operator is installed:

```bash
# Deploy operator (includes webhook deployment)
kubectl apply -k object-lease-operator/config/default

# Verify webhook is running
kubectl get pods -n object-lease-operator-system -l app=object-lease-webhook

# Check webhook configuration
kubectl get validatingwebhookconfiguration lease-validator.object-lease-controller.ullberg.io
```

## Troubleshooting

### Webhook Not Validating

1. **Check webhook is running:**
   ```bash
   kubectl get pods -n object-lease-operator-system -l app=object-lease-webhook
   ```

2. **Check LeaseController has webhook enabled:**
   ```bash
   kubectl get leasecontroller -o yaml
   # Look for spec.webhook.enabled: true
   ```

3. **Check ValidatingWebhookConfiguration exists:**
   ```bash
   kubectl get validatingwebhookconfiguration
   ```

### Certificate Issues

1. **Check certificate status:**
   ```bash
   kubectl get certificate -n object-lease-operator-system
   ```

2. **Check service-ca-operator is installed (OpenShift) or cert-manager:**
   ```bash
   # On OpenShift
   oc get pods -n openshift-service-ca-operator

   # Or with cert-manager
   kubectl get pods -n cert-manager
   ```

3. **View certificate details:**
   ```bash
   kubectl describe certificate object-lease-webhook-cert -n object-lease-operator-system
   ```

### Webhook Errors

View webhook logs:
```bash
kubectl logs -n object-lease-operator-system -l app=object-lease-webhook -f
```

Common errors:
- **"x509: certificate signed by unknown authority"**: Certificate not trusted, check service-ca-operator or cert-manager setup
- **"connection refused"**: Webhook service not reachable, check service and pod status
- **"context deadline exceeded"**: Webhook timeout, check webhook performance/resources

## Performance Considerations

- **Shared deployment**: One webhook serves all GVKs, reducing resource overhead
- **Caching**: ValidatingWebhookConfiguration is only updated when LeaseController CRs change
- **Efficient validation**: Only validates objects with TTL annotation present
- **High availability**: 2 replicas by default for reliability

## Cleanup and Deletion

### Automatic Cleanup with Finalizers

The webhook uses **finalizers** to ensure proper cleanup when a LeaseController CR is deleted:

1. **Finalizer added**: When webhook is enabled, a finalizer is added to the LeaseController CR
2. **CR deletion initiated**: When you delete the LeaseController, it enters "Terminating" state
3. **Webhook cleanup**: ConfigManager removes the GVK from ValidatingWebhookConfiguration
4. **Empty configuration**: If no more GVKs are configured, ValidatingWebhookConfiguration is deleted
5. **Finalizer removed**: After cleanup, the finalizer is removed
6. **CR deleted**: LeaseController CR is permanently deleted

**Benefits:**
- Guaranteed cleanup even if LeaseController is force-deleted
- No orphaned webhook rules in the cluster
- Automatic deletion of ValidatingWebhookConfiguration when last GVK is removed

### Verify Cleanup

```bash
# Delete a LeaseController
kubectl delete leasecontroller leasecontroller-deployments

# Watch for finalizer removal and cleanup
kubectl get leasecontroller leasecontroller-deployments -o yaml
# Look for: metadata.finalizers and metadata.deletionTimestamp

# Check ValidatingWebhookConfiguration is updated/deleted
kubectl get validatingwebhookconfiguration lease-validator.object-lease-controller.ullberg.io -o yaml
```

## Disabling the Webhook

To disable webhook validation for a specific GVK:

```yaml
apiVersion: object-lease-controller.ullberg.io/v1
kind: LeaseController
metadata:
  name: leasecontroller-deployments
spec:
  # ... other fields ...
  webhook:
    enabled: false  # Disable validation
```

To completely remove the webhook deployment, delete the webhook resources from the kustomization.

## Security

- Webhook runs as non-root user (65532)
- Minimal capabilities (drops ALL)
- Read-only root filesystem
- Seccomp profile enabled
- TLS encryption required for all webhook calls
