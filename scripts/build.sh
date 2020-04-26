#!/usr/bin/env bash

set -o errexit -o nounset -o pipefail -o posix

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "${REPO_ROOT}"

CGO_ENABLED=0
export CGO_ENABLED

PLATFORMS=(
  "linux/amd64"
  "darwin/amd64"
)

for PLATFORM in ${PLATFORMS[@]} ; do
  GOOS="$(dirname "${PLATFORM}")"
  export GOOS
  GOARCH="$(basename "${PLATFORM}")"
  export GOARCH

  mkdir -p "bin/${PLATFORM}"
  for CMD_DIR in cmd/*/ ; do
    CMD="$(basename "${CMD_DIR}")"
    echo "Building: bin/${PLATFORM}/${CMD}"
    go build -o "bin/${PLATFORM}/${CMD}" "./cmd/${CMD}"
  done
done
