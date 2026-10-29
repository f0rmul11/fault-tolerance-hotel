#!/usr/bin/env bash
# Only the control planes: no ingress gateway, Kiali, Argo CD or Prometheus.
set -euo pipefail
: "${1:?usage: install.sh KUBECONFIG TOOLS_DIR}"
: "${2:?usage: install.sh KUBECONFIG TOOLS_DIR}"
CANARY_KUBECONFIG="$1"
CANARY_TOOLS="$2"
CANARY_ROOT="$(cd "$(dirname "$0")" && pwd)"
ISTIO_VERSION=1.31.3
ROLLOUTS_VERSION=v1.10.1
command -v kubectl >/dev/null || { echo 'Установите kubectl' >&2; exit 1; }
command -v curl >/dev/null || { echo 'Установите curl' >&2; exit 1; }
case "$(uname -s)-$(uname -m)" in
  Darwin-arm64) CANARY_PLATFORM=osx-arm64 ;;
  Darwin-x86_64) CANARY_PLATFORM=osx-amd64 ;;
  Linux-aarch64|Linux-arm64) CANARY_PLATFORM=linux-arm64 ;;
  Linux-x86_64) CANARY_PLATFORM=linux-amd64 ;;
  *) echo 'Поддерживаются macOS/Linux amd64/arm64' >&2; exit 1 ;;
esac
CANARY_ISTIO_DIR="$CANARY_TOOLS/istio-$ISTIO_VERSION-$CANARY_PLATFORM"
if [ ! -x "$CANARY_ISTIO_DIR/istioctl" ]; then
  mkdir -p "$CANARY_TOOLS"
  CANARY_TMP="$(mktemp -d "$CANARY_TOOLS/istio-download.XXXXXX")"
  trap 'rm -rf "$CANARY_TMP"' EXIT
  curl --fail --location --retry 3 --connect-timeout 15 --max-time 300 \
    "https://github.com/istio/istio/releases/download/$ISTIO_VERSION/istioctl-$ISTIO_VERSION-$CANARY_PLATFORM.tar.gz" \
    -o "$CANARY_TMP/istioctl.tar.gz"
  curl --fail --location --retry 3 --connect-timeout 15 --max-time 60 \
    "https://github.com/istio/istio/releases/download/$ISTIO_VERSION/istioctl-$ISTIO_VERSION-$CANARY_PLATFORM.tar.gz.sha256" \
    -o "$CANARY_TMP/checksum"
  CANARY_EXPECTED="$(awk '{print $1}' "$CANARY_TMP/checksum")"
  CANARY_ACTUAL="$(shasum -a 256 "$CANARY_TMP/istioctl.tar.gz" | awk '{print $1}')"
  [ "$CANARY_ACTUAL" = "$CANARY_EXPECTED" ] || { echo 'Checksum istioctl не совпал' >&2; exit 1; }
  tar -xzf "$CANARY_TMP/istioctl.tar.gz" -C "$CANARY_TMP" istioctl
  mkdir -p "$CANARY_ISTIO_DIR"
  mv "$CANARY_TMP/istioctl" "$CANARY_ISTIO_DIR/istioctl"
fi
"$CANARY_ISTIO_DIR/istioctl" --kubeconfig "$CANARY_KUBECONFIG" install -y -f "$CANARY_ROOT/istio.yaml"
kubectl --kubeconfig "$CANARY_KUBECONFIG" create namespace argo-rollouts --dry-run=client -o yaml |
  kubectl --kubeconfig "$CANARY_KUBECONFIG" apply -f -
kubectl --kubeconfig "$CANARY_KUBECONFIG" apply --server-side -n argo-rollouts \
  -f "https://github.com/argoproj/argo-rollouts/releases/download/$ROLLOUTS_VERSION/install.yaml"
kubectl --kubeconfig "$CANARY_KUBECONFIG" wait --for=condition=Established \
  crd/rollouts.argoproj.io crd/virtualservices.networking.istio.io crd/destinationrules.networking.istio.io --timeout=120s
kubectl --kubeconfig "$CANARY_KUBECONFIG" rollout status deployment/argo-rollouts -n argo-rollouts --timeout=180s
