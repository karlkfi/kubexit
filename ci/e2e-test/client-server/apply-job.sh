#!/usr/bin/env bash

set -o errexit -o nounset -o pipefail -o posix

cd "$(dirname "${BASH_SOURCE[0]}")"

IMAGE_TAG=latest
if [[ -n "${GITHUB_SHA:-}" ]]; then
  IMAGE_TAG="sha-${GITHUB_SHA:0:7}"
fi

echo "Image: karlkfi/kubexit:${IMAGE_TAG}"
kustomize edit set image "karlkfi/kubexit=karlkfi/kubexit:${IMAGE_TAG}"
kustomize edit set image "karlkfi/test-client=karlkfi/test-client:${IMAGE_TAG}"
kustomize build . | kubectl apply -f -
