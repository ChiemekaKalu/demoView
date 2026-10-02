"""M3: rounds like this one. Embeds every regulation round's name-free story
with a Workers AI embedding model and checks whether nearest neighbours share
the same story: winner, how it was won, clutch, the order of the first deaths,
the buy matchup and the map. Each is compared with the chance that two random
rounds share it. The token comes from the environment and is never printed.

usage: embed.py <dir with facts/> <plan.json> <config.json> <out dir>
"""
import json
import os
import sys
import time
import urllib.request
from collections import Counter

import numpy as np

import analyze as A
import jev_pro as J


def story(m, r):
    kills = [k for k in r.get("kills") or [] if not k.get("post")]
    lines = [(k["t"], J.kill_line(k)) for k in kills]
    if r.get("plant"):
        p = r["plant"]
        lines.append((p["t"] - 0.01, f"{J.clock(p['t'])} the T side plants the bomb" + (f" at {p['site']} site." if p.get("site") else ".")))
    if r.get("defuse"):
        lines.append((r["defuse"]["t"], f"{J.clock(r['defuse']['t'])} the CT side defuses the bomb."))
    if r.get("explode") is not None:
        lines.append((r["explode"], f"{J.clock(r['explode'])} the bomb explodes."))
    e = r.get("econ") or {}
    buys = f"CT {A.buy(e.get('CT', {}).get('equip', 0), r['n'] in (1, 13))}, T {A.buy(e.get('T', {}).get('equip', 0), r['n'] in (1, 13))}"
    return f"de_{m}. Buys: {buys}. " + " ".join(l for _, l in sorted(lines)) + f" Result: {r['winner']} win by {r['reason'].replace('_', ' ')}."


def attrs(m, r):
    kills = [k for k in r.get("kills") or [] if not k.get("post")]
    planted = r.get("plant") is not None
    w = r["winner"]
    how = ("ct_time" if r["reason"] == "time_expired" else ("t_post_plant" if w == "T" else "ct_retake") if planted
           else ("t_before_plant" if w == "T" else "ct_hold"))
    clutch = any(((k.get("a") or [0, 0])[0 if w == "CT" else 1] == 1 and (k.get("a") or [0, 0])[1 if w == "CT" else 0] >= 1) for k in kills)
    e = r.get("econ") or {}
    return {"winner": w, "how_won": how, "clutch": clutch, "first_deaths": "".join((k.get("vs") or "?")[0] for k in kills[:3]),
            "buys": (A.buy(e.get("CT", {}).get("equip", 0), r["n"] in (1, 13)), A.buy(e.get("T", {}).get("equip", 0), r["n"] in (1, 13))),
            "map": m}


def embed(texts, model, batch):
    url = f"https://api.cloudflare.com/client/v4/accounts/{os.environ['CLOUDFLARE_ACCOUNT_ID']}/ai/run/{model}"
    out, calls, ms = [], 0, []
    for i in range(0, len(texts), batch):
        body = json.dumps({"text": texts[i:i + batch]}).encode()
        for attempt in range(5):
            t0 = time.time()
            try:
                req = urllib.request.Request(url, data=body, method="POST", headers={"Authorization": f"Bearer {os.environ['CLOUDFLARE_JEV']}", "Content-Type": "application/json"})
                with urllib.request.urlopen(req, timeout=120) as r:
                    out += json.load(r)["result"]["data"]
                calls += 1
                ms.append(round((time.time() - t0) * 1000))
                break
            except Exception:
                time.sleep(2 + 3 * attempt)
        else:
            raise RuntimeError("embedding batch failed")
    return np.array(out, dtype=np.float32), {"calls": calls, "p50_ms": sorted(ms)[len(ms) // 2] if ms else None}


def main():
    src, plan_path, cfg_path, out = sys.argv[1:5]
    plan, cfg = json.load(open(plan_path)), json.load(open(cfg_path))["models"]
    os.makedirs(out, exist_ok=True)
    demos, _ = A.load(src, plan)
    items = [(d["id"], d["map_name"], r) for d in demos for r in d["rounds"] if r["n"] <= 24]
    texts = [story(m, r) for _, m, r in items]
    meta = [attrs(m, r) for _, m, r in items]
    vecs, ledger = embed(texts, cfg["embed_model"], cfg["embed_batch"])
    vecs /= np.linalg.norm(vecs, axis=1, keepdims=True) + 1e-9
    sims = vecs @ vecs.T
    demo_ids = np.array([i for i, _, _ in items])
    np.fill_diagonal(sims, -2)
    k = 5
    res = {"rounds": len(items), "model": cfg["embed_model"], "ledger": ledger, "agreement_at_5": {}, "agreement_at_5_other_demos": {}, "chance": {}}
    nn_all = np.argsort(-sims, axis=1)[:, :k]
    other = sims.copy()
    other[demo_ids[:, None] == demo_ids[None, :]] = -2
    nn_other = np.argsort(-other, axis=1)[:, :k]
    for a in ("winner", "how_won", "clutch", "first_deaths", "buys", "map"):
        vals = [m[a] for m in meta]
        cnt = Counter(vals)
        res["chance"][a] = round(sum((c / len(vals)) ** 2 for c in cnt.values()), 3)
        for key, nn in (("agreement_at_5", nn_all), ("agreement_at_5_other_demos", nn_other)):
            res[key][a] = round(float(np.mean([[vals[i] == vals[j] for j in row] for i, row in enumerate(nn)])), 3)
    # A few examples: the round, then its three nearest neighbours in other demos.
    examples = []
    picks = [i for i, m in enumerate(meta) if m["clutch"]][:1] + [i for i, m in enumerate(meta) if m["how_won"] == "ct_retake"][:1] + \
            [i for i, m in enumerate(meta) if m["buys"][0] in ("eco", "semi") and m["winner"] == "CT"][:1]
    for i in picks:
        examples.append({"query": {"map": meta[i]["map"], "how_won": meta[i]["how_won"], "text": texts[i][:600]},
                         "neighbours": [{"map": meta[j]["map"], "how_won": meta[j]["how_won"], "similarity": round(float(other[i, j]), 3), "text": texts[j][:400]} for j in nn_other[i][:3]]})
    res["examples"] = examples
    json.dump(res, open(os.path.join(out, "embed_report.json"), "w"), indent=1)
    L = ["# Rounds like this one", "", f"{res['rounds']} regulation rounds embedded with {res['model']} ({ledger['calls']} calls).", "",
         "| Shared attribute | Top-5 neighbours | Top-5, other demos only | Two random rounds |", "| --- | --- | --- | --- |"]
    for a in res["chance"]:
        L.append(f"| {a} | {res['agreement_at_5'][a]:.0%} | {res['agreement_at_5_other_demos'][a]:.0%} | {res['chance'][a]:.0%} |")
    open(os.path.join(out, "embed.md"), "w").write("\n".join(L) + "\n")
    print("\n".join(L))


if __name__ == "__main__":
    main()
