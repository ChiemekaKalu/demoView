"""Picks a deterministic, map-balanced sample of pro demos from the Hugging Face
dataset and deals it into shards. Writes plan.json to stdout.

usage: select.py config.json
"""
import json
import random
import re
import sys
import urllib.request

API = "https://huggingface.co/api/datasets/"


def get(url):
    with urllib.request.urlopen(url, timeout=60) as r:
        return json.load(r)


def main():
    cfg = json.load(open(sys.argv[1]))
    ds = cfg["dataset"]
    revision = get(API + ds)["sha"]
    tree = get(f"{API}{ds}/tree/{revision}/demos?recursive=true")
    by_map = {}
    for x in tree:
        if x.get("type") != "file" or not x["path"].endswith(".dem"):
            continue
        m = re.search(r"-([a-z0-9]+)\.dem$", x["path"])
        name = m.group(1) if m else "unknown"
        if name == "unknown":
            continue
        size = (x.get("lfs") or {}).get("size", x.get("size", 0))
        series = x["path"].split("/")[1] if x["path"].count("/") >= 2 else x["path"]
        by_map.setdefault(name, []).append({"path": x["path"], "map": name, "size": size, "series": series})

    rng = random.Random(cfg["seed"])
    picked = []
    for name in sorted(by_map):
        pool = sorted(by_map[name], key=lambda x: x["path"])
        rng.shuffle(pool)
        seen, first, rest = set(), [], []
        for x in pool:  # one map per series first, so a long series doesn't dominate
            (rest if x["series"] in seen else first).append(x)
            seen.add(x["series"])
        picked.append((first + rest)[: cfg["per_map"]])

    items = []
    for i in range(max(len(p) for p in picked)):  # interleave maps so every shard gets a mix
        for p in picked:
            if i < len(p):
                items.append(p[i])
    for i, x in enumerate(items):
        x["shard"] = i % cfg["shards"]
    json.dump({"dataset": ds, "revision": revision, "seed": cfg["seed"], "per_map": cfg["per_map"],
               "shards": cfg["shards"], "items": items}, sys.stdout, indent=1)


if __name__ == "__main__":
    main()
