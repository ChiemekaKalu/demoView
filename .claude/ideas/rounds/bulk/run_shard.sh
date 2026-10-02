#!/usr/bin/env bash
# Streams one shard's demos, one at a time: download, extract facts, export
# with the oracle, run our parser and diff it against the oracle, record, then
# delete the demo. Only facts and the parser record are kept.
#
# usage: run_shard.sh <shard> <plan.json> <bin dir> <out dir>
# SKIP_FACTS=1 runs only the parser checks (parser mode).
set -uo pipefail

shard=$1 plan=$2 bin=$3 out=$4
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$out/facts" "$out/parser" "$out/diffs" "$out/events"
events=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("parser_events") or "")' "$plan")

dataset=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["dataset"])' "$plan")
revision=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["revision"])' "$plan")
base="https://huggingface.co/datasets/$dataset/resolve/$revision"

python3 - "$plan" "$shard" > "$work/list.tsv" <<'EOF'
import json, sys
for it in json.load(open(sys.argv[1]))["items"]:
    if it["shard"] == int(sys.argv[2]):
        print(it["path"], it["map"], it["size"], sep="\t")
EOF

# timed <name> <cmd...>: runs cmd, keeps "<exit code> <seconds> <max rss kb>" in $work/<name>.t
timed() {
  local name=$1; shift
  /usr/bin/time -f "%e %M" -o "$work/$name.time" "$@"
  local rc=$?
  echo "$rc $(tail -n 1 "$work/$name.time")" > "$work/$name.t"
}

while IFS=$'\t' read -r path map size; do
  id=$(printf '%s' "$path" | sha1sum | cut -c1-12)
  rm -rf "$work/ref" "$work/ours" "$work"/*.t "$work"/*.err "$work/diff.txt"
  start=$(date +%s.%N)
  if ! curl -sfL --retry 3 --retry-delay 5 -o "$work/d.dem" "$base/$path"; then
    python3 "$here/record.py" "$path" "$id" "$map" "$size" -1 "$work" "$out" >> "$out/parser/$shard.jsonl"
    continue
  fi
  dl=$(python3 -c "import time,sys; print(round(time.time()-float(sys.argv[1]),1))" "$start")
  if [ -z "${SKIP_FACTS:-}" ]; then
    timed facts "$bin/bulkfacts" "$work/d.dem" > "$out/facts/$id.json" 2> "$work/facts.err"
  fi
  timed oracle "$bin/oracle" export "$work/d.dem" --out "$work/ref" 2> "$work/oracle.err"
  timed parse "$bin/demo" parse "$work/d.dem" --out "$work/ours" 2> "$work/parse.err"
  if [ -n "$events" ]; then
    # Round-structure timeline from the parser under test: ticks and event names only.
    "$bin/demo" inspect --events "$work/d.dem" > "$out/events/$id.events.txt" 2> "$work/events.err" || true
    grep -Eq '7656119[0-9]{10}' "$out/events/$id.events.txt" && rm -f "$out/events/$id.events.txt"
  fi
  if [ -f "$work/ref/match.json" ]; then
    timed diff "$bin/demo" diff "$work/d.dem" --against "$work/ref/match.json" > "$work/diff.txt" 2> "$work/diff.err"
    cp "$work/diff.txt" "$out/diffs/$id.txt"  # Actions artifact only: a diff can name players
  fi
  python3 "$here/record.py" "$path" "$id" "$map" "$size" "$dl" "$work" "$out" >> "$out/parser/$shard.jsonl"
  rm -f "$work/d.dem"
  echo "shard $shard: $map $(tail -n 1 "$out/parser/$shard.jsonl" | cut -c1-160)"
done < "$work/list.tsv"
