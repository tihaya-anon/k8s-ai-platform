#!/usr/bin/env bash
set -euo pipefail

KIND_VERSION=${KIND_VERSION:-v0.32.0}
KUBEBUILDER_VERSION=${KUBEBUILDER_VERSION:-4.9.0}
KUBECTL_VERSION=${KUBECTL_VERSION:-v1.34.8}
TARGET_ARCH=${TARGET_ARCH:-amd64}

curl -fsSLo /usr/local/bin/kind \
  "https://kind.sigs.k8s.io/dl/${KIND_VERSION}/kind-linux-${TARGET_ARCH}"
chmod +x /usr/local/bin/kind

curl -fsSLo /tmp/kubebuilder \
  "https://go.kubebuilder.io/dl/${KUBEBUILDER_VERSION}/linux/${TARGET_ARCH}"
chmod +x /tmp/kubebuilder
mv /tmp/kubebuilder /usr/local/bin/kubebuilder

curl -fsSLo /usr/local/bin/kubectl \
  "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${TARGET_ARCH}/kubectl"
chmod +x /usr/local/bin/kubectl

docker network create -d bridge --subnet 172.19.0.0/24 kind 2>/dev/null || true

go version
kind version
kubebuilder version
kubectl version --client
docker --version