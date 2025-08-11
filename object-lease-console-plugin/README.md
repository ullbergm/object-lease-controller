# Object Lease Console Plugin

Adds a navigation section and page to the OpenShift Console that lists Kubernetes Lease resources the current user can access.

## Dev

- Requires Node >= 22 and Yarn classic.
- Start a local dev server:

```
yarn install
yarn start
```

Run Bridge with plugin enabled (example):

- Start console (bridge) with plugin param and i18n namespace:
  -plugins object-lease-console-plugin=http://localhost:9001/ -i18n-namespaces=plugin__object-lease-console-plugin

Then open the Console and visit /object-lease/leases.

## Build image

```
yarn install
yarn build
docker build -t ghcr.io/ullbergm/object-lease-console-plugin:latest .
```

## Deploy on cluster

The kustomize in k8s/ includes:
- Deployment and Service (HTTPS with service CA)
- ConsolePlugin CR

This repo wires it into object-lease-operator/config/default so `kubectl apply -k` deploys both operator and plugin.
