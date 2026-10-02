#!/usr/bin/env bash
# Fetches a stored run's plan and shard results from the study's R2 prefix.
#
# usage: collect.sh <source run id> <dir>
set -uo pipefail

src=$1 dir=$2
here=$(cd "$(dirname "$0")" && pwd)
mkdir -p "$dir/in"
bash "$here/r2get.sh" "$src/plan.json" "$dir/plan.json" || exit 1
n=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["shards"])' "$dir/plan.json")
for i in $(seq 0 $((n - 1))); do
  if bash "$here/r2get.sh" "$src/shards/$i.tar.gz" "$dir/s$i.tar.gz"; then
    tar -xzf "$dir/s$i.tar.gz" -C "$dir/in"
  else
    echo "shard $i missing"
  fi
done
