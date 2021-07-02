#!/usr/bin/env bash

set -o errexit -o nounset -o pipefail -o posix

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd -P)"
cd "${REPO_ROOT}"

echo "Awaiting Job Init..."
kubectl wait pod --selector=job-name=client-server-job --for=condition=Initialized --timeout=2m

echo
echo "Awaiting Job Completed or Error..."
timeout=120
SECONDS=0
while (( SECONDS < timeout )); do
    job_status="$(kubectl get pods --selector=job-name=client-server-job -o jsonpath="{.items[*].status.containerStatuses[*].state.terminated.reason}")"
    if [[ "${job_status}" == "Completed Completed" || "${job_status}" == *"Error" ]]; then
        echo "Status: ${job_status}"
        break
    fi
    echo "Sleeping 2s..."
    sleep 2
done

echo
echo "Container Logs:"
kubectl logs --selector=job-name=client-server-job --all-containers --tail=-1

echo
echo "Pod Respurces:"
kubectl get pods --selector=job-name=client-server-job -o json | jq '.items[].status'

echo "Status: ${job_status}"
if [[ "${job_status}" != *"Error" ]]; then
    echo "Expected: Error"
    exit 1
fi
