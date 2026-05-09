#!/usr/bin/env bash

set -o errexit -o nounset -o pipefail -o posix

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "${REPO_ROOT}"

# Print each workspace module directory, one per line.
sed -n '/^use *(/,/)/{ s/^[[:space:]]*//; s/)[[:space:]]*$//; p; }' go.work \
	| grep -v '^use' | grep -v '^)' | grep -v '^$'
