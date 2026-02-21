FROM golang:1.26

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1

# update PATH for local pip installs
ENV PATH="$PATH:~/.local/bin"

# Install golangci-lint
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl git xz-utils pre-commit gitleaks codespell shellcheck nodejs npm && rm -rf /var/lib/apt/lists/* \
    && GOLANGCI_LINT_VERSION=2.6.2 \
    && curl -sSLO "https://github.com/golangci/golangci-lint/releases/download/v${GOLANGCI_LINT_VERSION}/golangci-lint-${GOLANGCI_LINT_VERSION}-linux-amd64.tar.gz" \
    && tar -xzf "golangci-lint-${GOLANGCI_LINT_VERSION}-linux-amd64.tar.gz" -C /tmp \
    && mv "/tmp/golangci-lint-${GOLANGCI_LINT_VERSION}-linux-amd64/golangci-lint" /usr/local/bin/golangci-lint \
    && rm -rf "/tmp/golangci-lint-${GOLANGCI_LINT_VERSION}-linux-amd64*" "golangci-lint-${GOLANGCI_LINT_VERSION}-linux-amd64.tar.gz"

# Install OpenShift CLI (oc)
RUN OC_VERSION=latest \
    && curl -sSL -o /tmp/openshift-client-linux.tar.gz "https://mirror.openshift.com/pub/openshift-v4/clients/ocp/stable/openshift-client-linux.tar.gz" \
    && tar -xzf /tmp/openshift-client-linux.tar.gz -C /usr/local/bin oc kubectl \
    && rm /tmp/openshift-client-linux.tar.gz \
    && chmod +x /usr/local/bin/oc /usr/local/bin/kubectl

CMD sleep infinity

ENTRYPOINT []
