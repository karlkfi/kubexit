#!/usr/bin/env bash

set -o errexit -o nounset -o pipefail -o posix

cd "$(dirname "${BASH_SOURCE[0]}")"

kustomize edit set image karlkfi/kubexit=karlkfi/kubexit:latest
kustomize build . | kubectl apply -f -
