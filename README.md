# Object Lease Operator

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

#### object-lease-controller.ullberg.us/extended-at

If you set this value, the lease is calculated from this time rather from the creation time.
```bash
kubectl annotate pod test object-lease-controller.ullberg.us/extended-at=2026-06-11T20:48:11Z
```

#### object-lease-controller.ullberg.us/expire-at

This annotation is updated by the controller to show when the object will expire. This is meant to be used by systems that display information about objects in the environment.

#### object-lease-controller.ullberg.us/lease-status

This annotation is updated by the controller to indicate status or issues with the annotations. This is meant to be human readable information.

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

## Optimizations / Features

Here are some design decisions and optimizations:
- Controller instances are only managing a single GVK, providing separation of duty and scaling.
- Controllers use dedicated ServiceAccounts with permissions limited to the GVK they are managing.
- Reconcile loop knows when the object is going to expire and will not read the status from the object until it has expired or it has been updated.
- TTL can be added after the object has been created, in that case the expiration is based on the time the TTL annotation was added, rather than the object creation time.
