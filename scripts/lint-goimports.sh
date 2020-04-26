#!/usr/bin/env bash

set -o errexit -o nounset -o pipefail -o posix

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "${REPO_ROOT}"

# Dependencies
scripts/lib/require-cmd.sh find goimports

# Check gofmt
FILES="$(scripts/go-find.sh | xargs goimports -l)"
if [[ -n ${FILES} ]]; then
  echo 'The following files need formatting:'
  echo "${FILES}"
  echo "Use \`make goimports\` to reformat."
  exit 1
fi
