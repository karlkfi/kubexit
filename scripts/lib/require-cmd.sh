#!/usr/bin/env bash

set -o errexit -o nounset -o pipefail -o posix

# Validate that commands exist or error and exit
#
# Usage:
# ./require-cmd.sh [cmd...]
#

for cmd in "$@"; do
  if ! hash "${cmd}" 2> /dev/null; then
    echo >&2 "Error: ${cmd} not found"
    exit 1
  fi
done
