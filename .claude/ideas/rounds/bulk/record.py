"""Prints one JSON line describing what each tool did with one demo.

usage: record.py <path> <id> <map> <size> <download seconds, -1 if failed> <work dir> <out dir>
"""
import json
import os
import re
import sys


def step(work, name):
    t = os.path.join(work, name + ".t")
    if not os.path.exists(t):
        return None
    parts = open(t).read().split()
    rec = {"rc": int(parts[0])}
    if len(parts) >= 3:
        rec["s"] = float(parts[1])
        rec["rss_mb"] = round(int(parts[2]) / 1024)
    err = os.path.join(work, name + ".err")
    if os.path.exists(err):
        lines = [l for l in open(err, errors="replace").read().splitlines() if l.strip()]
        if lines:
            rec["err"] = lines[-1][:300]
    return rec


def main():
    path, id_, map_, size, dl, work, out = sys.argv[1:8]
    rec = {"path": path, "id": id_, "map": map_, "size_mb": round(int(size) / 1e6), "download_s": float(dl)}
    if float(dl) < 0:
        rec["download"] = "failed"
        print(json.dumps(rec))
        return
    for name in ("facts", "oracle", "parse", "diff"):
        s = step(work, name)
        if s is not None:
            rec[name] = s
    if "facts" in rec:
        facts = os.path.join(out, "facts", id_ + ".json")
        try:
            f = json.load(open(facts))
            rec["facts"].update(rounds=len(f["rounds"]), build=f.get("build"), facts_error=f.get("error"))
        except Exception as e:  # a crashed extractor leaves an empty or partial file
            rec["facts"]["read_error"] = str(e)[:200]
    diff = os.path.join(work, "diff.txt")
    if os.path.exists(diff):
        rec.setdefault("diff", {}).update(summarize(open(diff, errors="replace").read().splitlines()))
    print(json.dumps(rec))


def summarize(lines):
    """Name-free shape of a diff report: rounds found by each side, and how
    many mismatches of each kind, with player ids and indexes stripped."""
    out = {"report_lines": len(lines), "kinds": {}}
    for line in lines:
        m = re.search(r"rounds\.count: ours (\d+), reference (\d+)", line)
        if m:
            out["rounds"] = [int(m.group(1)), int(m.group(2))]
        if ": ours " in line:
            kind = re.sub(r"\[[^\]]*\]", "[]", line.strip().split(": ours ")[0])
            out["kinds"][kind] = out["kinds"].get(kind, 0) + 1
    return out


if __name__ == "__main__":
    main()
