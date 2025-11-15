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

### TTL units and formatting

The plugin shows the `object-lease-controller.ullberg.io/ttl` annotation value directly as provided on the resource. The controller (and the plugin) support a flexible duration format with case-insensitive units. Note the following rules:

- `m` (or `M`) means minutes.
- Use `mo`, `mth`, or `month` (case-insensitive) for months to avoid ambiguity with minutes.
- Units can be combined and fractional values are supported, for example:
  - `1h30m` (1 hour 30 minutes)
  - `0.5d` (12 hours)
  - `1mo2h` (one month and two hours)

The plugin will display the TTL string you annotated; it does not reformat or normalize it. Use the controller's TTL parser rules when constructing values.

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
