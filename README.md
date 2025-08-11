<h1 align="center">Object Lease Operator</h1>
<p align="center">
  <a target="_blank" href="https://github.com/ullbergm/object-lease-controller"><img src="https://img.shields.io/github/last-commit/ullbergm/object-lease-controller?logo=github&color=609966&logoColor=fff" alt="Last commit"/></a>
  <a href="https://github.com/ullbergm/object-lease-controller/blob/main/LICENSE"><img src="https://img.shields.io/badge/License-Apache%202.0-609966?logo=opensourceinitiative&logoColor=fff" alt="License Apache 2.0"/></a>
  <a href="https://codecov.io/gh/ullbergm/object-lease-controller"><img src="https://codecov.io/gh/ullbergm/object-lease-controller/graph/badge.svg?token=TUKIQAAR1R" alt="Test Coverage"/></a>
  <br />
  <a href="https://buymeacoffee.com/magnus.ullberg"><img src="https://img.shields.io/badge/Buy%20me%20a-coffee-ff1414.svg?color=aa1414&logoColor=fff&label=Buy%20me%20a" alt="buy me a coffee"/></a>
  <a href="https://ullberg.us/cv.pdf"><img src="https://img.shields.io/badge/Offer%20me%20a-job-00d414.svg?color=0000f4&logoColor=fff&label=Offer%20me%20a" alt="offer me a job"/></a>
</p>

This project implements a Kubernetes operator that allows you to specify a TTL (Time To Live) for an object and once that time passes the object is deleted. The operator dynamically deploys a controller for each Group-Version-Kind (GVK) you configure it to monitor. Each controller watches and manages resources of its assigned GVK, enabling scalable lease management across multiple resource types. The service account for the controller is granted a role the allows it to manage the specified GVK only.

## Features
- Deploys as an operator.
- Dynamically deploys a controller for each configured GVK.
- Controllers are only managing one GVK each, increasing scaleability.
- Leader election support for high availability.

## Architecture
The operator is designed to be highly extensible and scalable. For each GVK specified at startup, a dedicated controller is launched.

Each controller:
- Watches for changes to resources of its GVK.
- Manages lease of objects associated with those resources.
- Ensures lease lifecycle (renewal, expiration) is handled appropriately.

## Flow Diagram
![Object Lease Operator Flow](docs/object-lease-controller-flow.png)

## Configuration

### LeaseController
```yaml
---
apiVersion: object-lease-controller.ullberg.us/v1alpha1
kind: LeaseController
metadata:
  name: application-controller
spec:
  group: "startpunkt.ullberg.us"
  kind: "Application"
  version: "v1alpha2"
---
apiVersion: object-lease-controller.ullberg.us/v1alpha1
kind: LeaseController
metadata:
  name: deployment-controller
spec:
  group: ""
  kind: "Deployment"
  version: "v1"
```

### Object annotation
```yaml
apiVersion: startpunkt.ullberg.us/v1alpha2
kind: Application
metadata:
  name: google
  annotations:
    object-lease-controller.ullberg.us/ttl: "30m"
spec:
  name: Google
  url: https://google.com
```

### Annotations

#### object-lease-controller.ullberg.us/ttl

This will allow you to configure the time until the object will be deleted.
```bash
kubectl annotate pod test object-lease-controller.ullberg.us/ttl=1h30m
```

You can specify the time in hours, minutes, days, weeks, etc.

| Value   | Description      |
|---------|------------------|
| `2d`    | 2 days           |
| `1h30m` | 1 hour 30 minutes|
| `5m`    | 5 minutes        |
| `1w`    | 1 week           |
| `3h`    | 3 hours          |
| `10s`   | 10 seconds       |

### object-lease-controller.ullberg.us/lease-start

RFC3339 UTC timestamp. Single source of truth for when the lease started.

Controller behavior:

* If `ttl` exists and `lease-start` is missing or invalid, the controller sets `lease-start` to now.
* To extend a lease, delete `lease-start`. The controller sets it to now on the next reconcile.
* You can set `lease-start` explicitly to backdate or align with an external clock.

Examples:

```bash
# Extend now by resetting the start
kubectl annotate pod test object-lease-controller.ullberg.us/lease-start- --overwrite

# Set a specific start time
kubectl annotate pod test object-lease-controller.ullberg.us/lease-start=2025-01-01T12:00:00Z --overwrite
```

### object-lease-controller.ullberg.us/expire-at

Set by the controller. RFC3339 UTC timestamp for when the object will expire. Safe for dashboards to read.

### object-lease-controller.ullberg.us/lease-status

Set by the controller. Human readable status or validation errors.

### Removing TTL

Remove `ttl` to stop lease management. The controller clears `lease-start`, `expire-at`, and `lease-status`.

```bash
kubectl annotate pod test object-lease-controller.ullberg.us/ttl-
```

## Example Use Cases
- Automatically manage leases for custom resources (e.g., Applications, Databases, Services)
- Enforce expiration policies
- Integrate with external systems for lease validation or renewal

## Usage

### Build and Run controller locally
```bash
make build
# Example: monitor multiple GVKs by running multiple controllers
./bin/lease-controller -group startpunkt.ullberg.us -kind Application -version v1alpha2 -leader-elect -leader-elect-namespace default
./bin/lease-controller -group another.group -kind AnotherKind -version v1beta1 -leader-elect -leader-elect-namespace default
```

### Build and Run operator
```bash
cd object-lease-operator
make run
```

### Install OpenShift Console Plugin
```bash
kubectl -n object-lease-operator-system apply -k object-lease-console-plugin/k8s
```

## Behavior summary

* Add `ttl` to start management. Controller sets `lease-start` if missing.
* Delete `lease-start` to extend from now.
* Optionally set `lease-start` to a specific RFC3339 UTC time.
* Delete `ttl` to stop management. Controller removes lease annotations.
* Reconcile filters only react to changes in `ttl` and `lease-start`.
* The controller computes `expire-at` from `lease-start + ttl` and requeues until expiry.

## OpenShift User Workload Monitoring

If you want OpenShift User Workload Monitoring (UWM) to scrape your ServiceMonitor, ensure UWM is enabled and your operator’s namespace participates. Example:

```bash
# Enable UWM cluster-wide
oc -n openshift-monitoring patch configmap cluster-monitoring-config \
  --type merge -p '{"data":{"config.yaml":"enableUserWorkload: true\n"}}'

# Label your namespace if needed
oc label namespace <ns> 'openshift.io/user-monitoring=true' --overwrite
```

Details on enabling and namespace participation are documented by Red Hat:
- [Red Hat Documentation](https://docs.openshift.com/container-platform/latest/monitoring/enabling-monitoring-for-user-defined-projects.html)

That is all you need. Your counters and histogram are already registered on the default registry, so Prometheus will scrape them from `/metrics` on the ServiceMonitor endpoint.
