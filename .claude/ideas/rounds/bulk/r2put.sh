#!/usr/bin/env bash
# The study's only writer to R2: the dev bucket, under research/bulk/, nothing
# else. Results only, never demos.
#
# usage: r2put.sh <key under research/bulk/> <file>
set -euo pipefail

key=$1 file=$2
case "$key" in
  /* | *..* | *.dem) echo "r2put: refusing key $key" >&2; exit 2 ;;
esac
npx --yes wrangler@4.146.0 r2 object put "demoview-dev/research/bulk/$key" --file "$file" --remote
