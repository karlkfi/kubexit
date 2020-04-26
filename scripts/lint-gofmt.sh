#!/usr/bin/env bash

set -o errexit -o nounset -o pipefail -o posix

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "${REPO_ROOT}"

# Dependencies
scripts/lib/require-cmd.sh find gofmt

# Check gofmt
FILES="$(scripts/go-find.sh | xargs gofmt -l -s)"
if [[ -n ${FILES} ]]; then
  echo 'The following files need formatting:'
  echo "${FILES}"
  echo "Use \`make gofmt\` to reformat."
  exit 1
fi
