#!/usr/bin/env bash
set -eux

# initialize pre-commit
git config --global --add safe.directory /workspaces

# Install golangci-lint if not present (useful when devcontainer not rebuilt)
if ! command -v golangci-lint >/dev/null 2>&1; then
	echo "golangci-lint not found, installing v2 via github release tarball"
	# We separate update and install to avoid shellcheck SC2015 warnings
	apt-get update || true
	apt-get install -y --no-install-recommends ca-certificates curl xz-utils pre-commit gitleaks codespell shellcheck || true
	GOLANGCI_LINT_VERSION=2.6.2
	curl -sSLO "https://github.com/golangci/golangci-lint/releases/download/v${GOLANGCI_LINT_VERSION}/golangci-lint-${GOLANGCI_LINT_VERSION}-linux-amd64.tar.gz" || true
	tar -xzf golangci-lint-${GOLANGCI_LINT_VERSION}-linux-amd64.tar.gz -C /tmp || true
	mv /tmp/golangci-lint-${GOLANGCI_LINT_VERSION}-linux-amd64/golangci-lint /usr/local/bin/golangci-lint || true
	rm -rf /tmp/golangci-lint-${GOLANGCI_LINT_VERSION}-linux-amd64* golangci-lint-${GOLANGCI_LINT_VERSION}-linux-amd64.tar.gz || true
	echo "golangci-lint installed"
fi
