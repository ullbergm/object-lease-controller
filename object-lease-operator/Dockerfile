# Build the manager binary
FROM quay.io/operator-framework/helm-operator:v1.42.0@sha256:102abedc1eb4ece4187eab36107cb3176e672ff6bf17d64396cd903ce13affa0

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
