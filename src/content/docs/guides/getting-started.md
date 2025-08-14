---
title: Getting Started
description: A guide showing how to use the Object Lease Operator.
---

# Installation

## Installation via the Operator Hub interface
By adding a catalog source to the cluster, you are able to install and manage the operator through the regular Operator Hub interface.

> NOTE: The catalog is currently a preview feature and is being finalized. (See [#35](https://github.com/ullbergm/object-lease-controller/issues/35))

```yaml
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: object-lease-operator-catalog
  namespace: openshift-marketplace
spec:
  displayName: Object Lease Operator Catalog
  image: 'ghcr.io/ullbergm/object-lease-operator-catalog:latest'
  publisher: Magnus Ullberg
  sourceType: grpc
  updateStrategy:
    registryPoll:
      interval: 5m
```

# Configuration

Once the operator has been installed then you can configure the objects that it should be monitoring, for example:

## LeaseController
```yaml
---
apiVersion: object-lease-controller.ullberg.io/v1
kind: LeaseController
metadata:
  name: application-controller
spec:
  group: "startpunkt.ullberg.io"
  version: "v1alpha2"
  kind:
    singular: "Application"
    plural: "Applications"
---
apiVersion: object-lease-controller.ullberg.io/v1
kind: LeaseController
metadata:
  name: deployment-controller
spec:
  group: ""
  kind:
    singular: "Deployment"
    plural: "Deployments"
  version: "v1"
```

# Usage

With the operator configured to monitor a object type, you can add the annotation to the object that specifies how long it should be, before it is deleted.

## Object annotation
```yaml
apiVersion: startpunkt.ullberg.us/v1alpha2
kind: Application
metadata:
  name: google
  annotations:
    object-lease-controller.ullberg.io/ttl: "30m"
spec:
  name: Google
  url: https://google.com
```
