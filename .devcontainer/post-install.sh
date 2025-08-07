#!/usr/bin/env bash
set -e

apt-get update && apt-get install -y bash git make curl pre-commit

# Install kubectl
curl -fLo /usr/local/bin/kubectl https://dl.k8s.io/release/$(curl -Ls https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl
chmod +x /usr/local/bin/kubectl

# Install kustomize
curl -fLo kustomize.tar.gz https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize%2Fv5.7.0/kustomize_v5.7.0_linux_amd64.tar.gz
tar -xzf kustomize.tar.gz -C /usr/local/bin
rm kustomize.tar.gz

# Install Kubebuilder
curl -fLo /usr/local/bin/kubebuilder https://github.com/kubernetes-sigs/kubebuilder/releases/download/v4.6.0/kubebuilder_linux_amd64
chmod +x /usr/local/bin/kubebuilder

# Install controller-gen
go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.17.2

# Install OpenShift CLI
curl -fLo /tmp/oc.tar.gz "https://mirror.openshift.com/pub/openshift-v4/clients/oc/latest/linux/oc.tar.gz"
tar -xzf /tmp/oc.tar.gz -C /usr/local/bin
rm /tmp/oc.tar.gz
chmod +x /usr/local/bin/oc

# Install operator-sdk
curl -fLo /tmp/operator-sdk "https://github.com/operator-framework/operator-sdk/releases/download/v1.41.1/operator-sdk_linux_amd64"
chmod +x /tmp/operator-sdk
mv /tmp/operator-sdk /usr/local/bin/operator-sdk

# Install Kind
curl -fLo /tmp/kind "https://kind.sigs.k8s.io/dl/latest/kind-linux-amd64"
chmod +x /tmp/kind
mv /tmp/kind /usr/local/bin/kind

go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.3.1

#docker network create -d=bridge --subnet=172.19.0.0/24 kind

kind version
kubebuilder version
docker --version
go version
kubectl version --client
oc version --client
pre-commit install
pre-commit run --all-files
