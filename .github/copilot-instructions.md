# Object Lease Controller - AI Coding Agent Instructions

## Architecture Overview

This is a Kubernetes operator implementing **dynamic lease management** with a unique multi-controller architecture:

- **Root Controller**: Main binary (`cmd/main.go`) that runs one controller instance per GVK (Group-Version-Kind)
- **Operator**: Helm-based operator (`object-lease-operator/`) that deploys controller instances dynamically based on `LeaseController` CRDs
- **Console Plugin**: OpenShift UI extension (`object-lease-console-plugin/`) for lease visualization in OpenShift

The system uses **annotation-driven** lease management with these key annotations:
- `object-lease-controller.ullberg.io/ttl`: User-set lease duration (e.g., "30m", "2d")
- `object-lease-controller.ullberg.io/lease-start`: Controller-managed RFC3339 timestamp
- `object-lease-controller.ullberg.io/expire-at`: Controller-computed expiration time

## Critical Development Patterns

### Controller Architecture
Controllers are **GVK-specific** (`pkg/controllers/lease_controller.go`):
- Use `schema.GroupVersionKind` for dynamic resource watching
- Only reconcile when `ttl` or `lease-start` annotations change (see `onlyWithTTLAnnotation()` predicate)
- Implement namespace tracking via `util.NamespaceTracker` for opt-in label filtering

### Duration Parsing
Custom flexible duration parser (`pkg/util/duration.go`):
```go
// Supports: "2d", "1h30m", "1w", "30s"
ParseFlexibleDuration("2d") // returns 48 hours
```

### Metrics Pattern
Per-GVK metrics with **const labels** (`pkg/metrics/metrics.go`):
- Register metrics with GVK as const labels for proper Prometheus scoping
- Include `Info` gauge (always 1) to ensure metric family visibility

### Testing Conventions
- Use `testify/mock` for client mocking in controller tests
- Test annotation filtering logic extensively (see `lease_controller_test.go`)
- Mock `record.EventRecorder` for Kubernetes event testing

## Build & Development Workflows

### Local Development
```bash
# Controller (single GVK instance)
make run  # Runs with startpunkt.ullberg.us/Application/v1alpha2

# Operator (manages multiple controllers)
cd object-lease-operator && make run

# Console Plugin (requires OpenShift Console)
cd object-lease-console-plugin && yarn start
```

### Multi-Platform Builds
Both controller and operator use `docker buildx` for multi-arch:
- Controller: `PLATFORMS=linux/arm64,linux/amd64`
- Operator: `PLATFORMS=linux/arm64,linux/amd64`

### Testing Strategy
```bash
make test  # Includes race detection and coverage
```
- Controllers use extensive mocking (`controllers/*_test.go`)
- Utility functions have comprehensive unit tests (`util/*_test.go`)

## Integration Points

### Operator → Controller Deployment
Operator uses **Helm charts** (`object-lease-operator/helm-charts/leasecontroller/`) to deploy controller instances:
- Each `LeaseController` CR triggers a Helm release
- Controller deployment gets RBAC for specific GVK only
- Uses `watches.yaml` for CR-to-chart mapping

### Console Plugin → API Integration
Plugin (`object-lease-console-plugin/src/components/LeasesPage.tsx`):
- Discovers GVKs by reading `LeaseController` CRs
- Uses OpenShift Console SDK's `useK8sWatchResources` for multi-GVK watching
- Filters objects by lease annotation presence

### Prometheus Integration
- Controller exports metrics on `/metrics`
- OpenShift: Uses `ServiceMonitor` for User Workload Monitoring
- Metrics are GVK-scoped via const labels, not dynamic labels

## Project-Specific Conventions

### Annotation Management
Controllers follow **strict annotation lifecycle**:
1. User sets `ttl` → Controller sets `lease-start` if missing
2. User deletes `lease-start` → Controller resets to "now" (lease extension)
3. User deletes `ttl` → Controller removes all lease annotations

### Error Handling
Use `lease-status` annotation for user-visible errors (invalid TTL, etc.) rather than just logging.

### File Organization
- `pkg/controllers/`: Per-resource-type controllers
- `pkg/util/`: Shared utilities (duration parsing, deletion helpers)
- `pkg/metrics/`: Prometheus metrics per GVK
- `object-lease-operator/`: Separate operator codebase using Operator SDK
- `object-lease-console-plugin/`: React/TypeScript UI plugin

When modifying controllers, always update both the reconcile logic and corresponding predicate functions to maintain performance.
