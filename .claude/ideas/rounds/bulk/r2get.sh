#!/usr/bin/env bash
# Reads a result back from the study's prefix, for analyze-only runs.
#
# usage: r2get.sh <key under research/bulk/> <file>
set -euo pipefail

key=$1 file=$2
case "$key" in
  /* | *..*) echo "r2get: refusing key $key" >&2; exit 2 ;;
esac
npx --yes wrangler@4.146.0 r2 object get "demoview-dev/research/bulk/$key" --file "$file" --remote
