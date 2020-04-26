#!/usr/bin/env bash

set -o errexit -o nounset -o pipefail -o posix

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "${REPO_ROOT}"

# Dependencies
scripts/lib/require-cmd.sh go

# Go ./... automatically ignores /vendor/ files
# https://go-review.googlesource.com/c/go/+/38745/
FILES="$(go vet ./...)"
if [[ -n ${FILES} ]]; then
  echo 'The following files have problems:'
  echo "${FILES}"
  exit 1
fi
