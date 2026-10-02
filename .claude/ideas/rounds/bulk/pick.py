"""Picks a deterministic, map-balanced sample of pro demos from the Hugging Face
dataset, or takes an explicit list of paths, and deals it into shards. Writes
plan.json to stdout.

usage: pick.py config.json
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


def map_of(path):
    m = re.search(r"-([a-z0-9]+)\.dem$", path)
    return m.group(1) if m else "unknown"


def main():
    cfg = json.load(open(sys.argv[1]))
    ds = cfg["dataset"]
    revision = get(API + ds)["sha"]
    tree = get(f"{API}{ds}/tree/{revision}/demos?recursive=true")
    files = {x["path"]: x for x in tree if x.get("type") == "file" and x["path"].endswith(".dem")}

    parser_mode = cfg["mode"] == "parser"
    repro = parser_mode and cfg.get("parser_sample", "repro") == "repro"
    paths = cfg.get("paths") or (cfg.get("parser_paths") if repro else None)
    if paths:
        items = [{"path": p, "map": map_of(p), "size": (files[p].get("lfs") or {}).get("size", files[p].get("size", 0))}
                 for p in paths if p in files]
    else:
        by_map = {}
        for path, x in files.items():
            name = map_of(path)
            if name == "unknown":
                continue
            size = (x.get("lfs") or {}).get("size", x.get("size", 0))
            series = path.split("/")[1] if path.count("/") >= 2 else path
            by_map.setdefault(name, []).append({"path": path, "map": name, "size": size, "series": series})
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

    shards = min(cfg["parser_shards"] if parser_mode and paths else cfg["shards"], max(1, len(items)))
    for i, x in enumerate(items):
        x["shard"] = i % shards
    json.dump({"dataset": ds, "revision": revision, "seed": cfg["seed"], "per_map": cfg["per_map"],
               "shards": shards, "mode": cfg["mode"], "parser_ref": cfg.get("parser_ref"),
               "parser_events": bool(cfg.get("parser_events")), "items": items},
              sys.stdout, indent=1)


if __name__ == "__main__":
    main()
