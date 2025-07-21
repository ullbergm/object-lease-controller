# Build the manager binary
FROM quay.io/operator-framework/helm-operator:v1.41.1

### Required OpenShift Labels
LABEL name="Object Lease Operator" \
    maintainer="magnus@ullberg.us" \
    vendor="Magnus Ullberg" \
    version="v1.0.0" \
    release="1" \
    summary="Object Lease Operator" \
    description="Object Lease Operator is a operator which manages the time to live and expiration of objects on Kubernetes and OpenShift platforms."

ENV HOME=/opt/helm

COPY watches.yaml ${HOME}/watches.yaml
COPY helm-charts  ${HOME}/helm-charts

WORKDIR ${HOME}
