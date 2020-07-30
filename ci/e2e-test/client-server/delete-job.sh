#!/usr/bin/env bash

set -o errexit -o nounset -o pipefail -o posix

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd -P)"
cd "${REPO_ROOT}"

kubectl delete job client-server-job

echo
echo "Awaiting Pod Deletion..."
kubectl wait pods --for=delete --selector=job-name=client-server-job --timeout=2m