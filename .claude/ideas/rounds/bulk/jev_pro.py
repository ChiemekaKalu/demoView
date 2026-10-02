"""Jev on pro play: the CTs' win chance at round start and after every kill,
for a map-balanced sample of pro demos, built from the stored name-free facts.
Scores raw and recalibrated answers (the matchmaking temperature) against the
real winner, overall and per map, beside the logistic baseline. Time-boxed and
token-capped. The token comes from CLOUDFLARE_JEV and is never printed.

usage: jev_pro.py <dir with facts/> <plan.json> <config.json> <out dir>
"""
import json
import math
import os
import random
import sys
import threading
import time
import urllib.error
import urllib.request
from concurrent.futures import ThreadPoolExecutor

import analyze as A

QS = {
    "ct_wins": {"type": "noul", "instructions": "Will the CT side win this round from this moment?",
                "criteria": {"true": "The CTs win: they eliminate every T, defuse the bomb, or run out the clock with no plant",
                             "false": "The Ts win: they eliminate every CT or the bomb explodes"}},
    "tension": {"type": "score", "instructions": "How tense is this moment for a spectator?",
                "criteria": ["Decided: one side clearly has it", "Leaning one way", "Open: either side could win",
                             "Knife-edge: one duel decides it"]},
}
FLAGS = (("wb", "through a wall"), ("ns", "no-scope"), ("sm", "through smoke"), ("bl", "while blind"), ("fa", "flash-assisted"))


def clock(s):
    s = max(0, int(s))
    return f"{s // 60}:{s % 60:02d}"


def buy_label(total, pistol):
    return {"pistol": "pistol round", "eco": "eco (saving)", "semi": "semi-eco", "force": "force buy", "full": "full buy"}[A.buy(total, pistol)]


def kill_line(k):
    who = f"p{k['k']} ({k['ks']})" if k.get("k", -1) >= 0 and k.get("ks") else "the world"
    verb = "team-kills" if k.get("ks") and k.get("ks") == k.get("vs") else "kills"
    bits = [k.get("w") or "?"] + (["headshot"] if k.get("hs") else []) + [w for f, w in FLAGS if k.get(f)]
    where = f" at {k['vp']}" if k.get("vp") else ""
    a = k.get("a") or [0, 0]
    return f"{clock(k['t'])} {who} {verb} p{k['v']} ({k['vs']}){where} ({', '.join(bits)}). Alive: {a[0]} CT v {a[1]} T."


def states(m, r):
    """(cut kill index or -1 for round start, state) for one round."""
    pistol = r["n"] in (1, 13)
    econ = {}
    for s in ("CT", "T"):
        e = (r.get("econ") or {}).get(s, {})
        econ[s] = {"buy": buy_label(e.get("equip", 0), pistol), "rifles": e.get("rifles", 0), "awps": e.get("awps", 0), "armor": e.get("armor", 0)}
        if s == "CT":
            econ[s]["kits"] = e.get("kits", 0)
    base = {"map": "de_" + m, "round": str(r["n"]), "score_before_round": f"CT {r.get('score_ct', 0)} - T {r.get('score_t', 0)}", "economy": econ}
    kills = [k for k in r.get("kills") or [] if not k.get("post")]
    plant = r.get("plant")
    out = [(-1, dict(base, timeline=["Freeze time ends."], now={"time": "about 115 seconds left in the round", "alive": "5 CT v 5 T"}))]
    for i, k in enumerate(kills):
        t = k["t"]
        lines = [(x["t"], kill_line(x)) for x in kills[: i + 1]]
        if plant and plant["t"] <= t:
            site = f" at bombsite {plant['site']}" if plant.get("site") else ""
            lines.append((plant["t"] - 0.01, f"{clock(plant['t'])} p{plant['p']} (T) plants the bomb{site}. The CTs have 40 seconds to defuse."))
            when = f"bomb planted, about {max(0, round(40 - (t - plant['t'])))} seconds until it explodes"
        else:
            when = f"about {max(0, round(115 - t))} seconds left in the round"
        a = k.get("a") or [0, 0]
        out.append((i, dict(base, timeline=[l for _, l in sorted(lines)], now={"time": when, "alive": f"{a[0]} CT v {a[1]} T"})))
    return out


class Jev:
    def __init__(self, rps, max_tokens):
        self.url = f"https://api.cloudflare.com/client/v4/accounts/{os.environ['CLOUDFLARE_ACCOUNT_ID']}/ai/run"
        self.token = os.environ["CLOUDFLARE_JEV"]
        self.gap, self.max_tokens = 1.0 / rps, max_tokens
        self.lock, self.next_at = threading.Lock(), 0.0
        self.tokens = self.calls = self.errors = self.retries = self.r429 = 0
        self.ms = []
        self.model = None

    def ask(self, state):
        if self.tokens >= self.max_tokens:
            raise RuntimeError("token cap")
        body = json.dumps({"model": "typesafe/jev", "input": {"state": state, "questions": QS}}).encode()
        for attempt in range(6):
            with self.lock:
                wait = self.next_at - time.time()
                self.next_at = max(time.time(), self.next_at) + self.gap
            if wait > 0:
                time.sleep(wait)
            req = urllib.request.Request(self.url, data=body, method="POST",
                                         headers={"Authorization": f"Bearer {self.token}", "Content-Type": "application/json"})
            t0 = time.time()
            try:
                with urllib.request.urlopen(req, timeout=60) as resp:
                    res = (json.load(resp).get("result") or {}).get("result") or {}
                with self.lock:
                    self.calls += 1
                    self.ms.append(round((time.time() - t0) * 1000))
                    self.tokens += (res.get("usage") or {}).get("input_tokens", 0)
                    self.model = res.get("model") or self.model
                return res["answers"]
            except urllib.error.HTTPError as e:
                with self.lock:
                    self.r429 += e.code == 429
                    self.retries += 1
                if e.code < 500 and e.code != 429:
                    break
            except Exception:
                with self.lock:
                    self.retries += 1
            time.sleep(1 + 2 * attempt)
        with self.lock:
            self.errors += 1
        raise RuntimeError("jev call failed")


def score(ps, ys):
    e, tab = A_ece(ps, ys)
    return {"n": len(ys), "brier": round(sum((p - y) ** 2 for p, y in zip(ps, ys)) / len(ys), 4),
            "auc": round(auc(ps, ys), 3), "ece": round(e, 3), "calib": tab}


def A_ece(ps, ys, bins=10):
    b = {}
    for p, y in zip(ps, ys):
        b.setdefault(min(int(p * bins), bins - 1), []).append((p, y))
    tab = [(k / bins, len(v), round(sum(p for p, _ in v) / len(v), 3), round(sum(y for _, y in v) / len(v), 3)) for k, v in sorted(b.items())]
    return sum(n * abs(mp - my) for _, n, mp, my in tab) / len(ps), tab


def auc(ps, ys):
    pairs = sorted(zip(ps, ys))
    ranks, i = [0.0] * len(pairs), 0
    while i < len(pairs):
        j = i
        while j + 1 < len(pairs) and pairs[j + 1][0] == pairs[i][0]:
            j += 1
        for k in range(i, j + 1):
            ranks[k] = (i + j) / 2 + 1
        i = j + 1
    pos = sum(1 for _, y in pairs if y)
    neg = len(pairs) - pos
    return (sum(r for r, (_, y) in zip(ranks, pairs) if y) - pos * (pos + 1) / 2) / (pos * neg) if pos and neg else 0.5


def cal(p, t):
    p = min(max(p, 1e-3), 1 - 1e-3)
    return 1 / (1 + math.exp(-math.log(p / (1 - p)) / t))


def main():
    src, plan_path, cfg_path, out = sys.argv[1:5]
    plan, cfg = json.load(open(plan_path)), json.load(open(cfg_path))["jev"]
    os.makedirs(out, exist_ok=True)
    demos, _ = A.load(src, plan)
    rng = random.Random(cfg["seed"])
    by_map = {}
    for d in sorted(demos, key=lambda d: d["id"]):
        by_map.setdefault(d["map_name"], []).append(d)
    for v in by_map.values():
        rng.shuffle(v)
    sample, i = [], 0
    while len(sample) < cfg["demos"] and any(i < len(v) for v in by_map.values()):
        for m in sorted(by_map):
            if i < len(by_map[m]) and len(sample) < cfg["demos"]:
                sample.append(by_map[m][i])
        i += 1
    picked = {d["id"] for d in sample}

    tasks = []
    for d in sample:
        for r in d["rounds"]:
            if r["n"] > 24:
                continue
            y = 1 if r["winner"] == "CT" else 0
            kills = [k for k in r.get("kills") or [] if not k.get("post")]
            plant_t = (r.get("plant") or {}).get("t")
            for cut, st in states(d["map_name"], r):
                k = kills[cut] if cut >= 0 else None
                a = (k or {}).get("a") or [5, 5]
                tasks.append({"map": d["map_name"], "demo": d["id"], "n": r["n"], "y": y, "ct": a[0], "t": a[1],
                              "planted": int(plant_t is not None and k is not None and plant_t <= k["t"]),
                              "sec": k["t"] if k else 0.0, "kind": "kill" if k else "start",
                              "equip_ct": (r.get("econ") or {}).get("CT", {}).get("equip", 0),
                              "equip_t": (r.get("econ") or {}).get("T", {}).get("equip", 0), "state": st})
    jev = Jev(cfg["rps"], cfg["max_input_tokens"])
    deadline = time.time() + cfg["minutes"] * 60

    def run(x):
        if time.time() > deadline:
            return None
        try:
            a = jev.ask(x["state"])
            return dict({k: v for k, v in x.items() if k != "state"}, p=a["ct_wins"]["noul"], tension=a["tension"]["score"])
        except Exception:
            return None

    with ThreadPoolExecutor(cfg["workers"]) as ex:
        done = [r for r in ex.map(run, tasks) if r]

    # Baseline: the logistic model trained on every demo outside the sample.
    rows = A.round_rows([d for d in demos if d["id"] not in picked])
    train = A.moments(rows)
    maps = sorted({x["map"] for x in train})
    w = A.fit([A.feats(x, "econ", maps) for x in train], [x["y"] for x in train])
    ys = [x["y"] for x in done]
    raw = [x["p"] for x in done]
    t_mm = cfg["temperature"]
    best_t = min((sum((cal(p, t) - y) ** 2 for p, y in zip(raw, ys)), t) for t in [0.2 + 0.02 * i for i in range(91)])[1] if done else None
    rep = {"demos": len(sample), "moments_planned": len(tasks), "moments_scored": len(done), "model": jev.model,
           "temperature_matchmaking": t_mm, "temperature_fitted_on_pro": best_t,
           "ledger": {"calls": jev.calls, "errors": jev.errors, "retries": jev.retries, "http_429": jev.r429,
                      "input_tokens": jev.tokens, "usd": round(jev.tokens * 0.042e-6, 4),
                      "p50_ms": sorted(jev.ms)[len(jev.ms) // 2] if jev.ms else None,
                      "p95_ms": sorted(jev.ms)[int(len(jev.ms) * 0.95)] if jev.ms else None},
           "overall": {}, "per_map": {}, "tension_vs_closeness": None}
    if done:
        base = [A.predict(w, A.feats(x, "econ", maps)) for x in done]
        rep["overall"] = {"jev_raw": score(raw, ys), "jev_T_matchmaking": score([cal(p, t_mm) for p in raw], ys),
                          "jev_T_pro": score([cal(p, best_t) for p in raw], ys), "logistic_econ": score(base, ys)}
        for m in sorted({x["map"] for x in done}):
            idx = [i for i, x in enumerate(done) if x["map"] == m]
            yy = [ys[i] for i in idx]
            if len(set(yy)) > 1:
                rep["per_map"][m] = {"moments": len(idx), "jev_T_matchmaking": score([cal(raw[i], t_mm) for i in idx], yy)["brier"],
                                     "logistic_econ": score([base[i] for i in idx], yy)["brier"]}
        close = [1 - 2 * abs(cal(x["p"], t_mm) - 0.5) for x in done]
        ten = [x["tension"] for x in done]
        mc, mt = sum(close) / len(close), sum(ten) / len(ten)
        cov = sum((a - mc) * (b - mt) for a, b in zip(close, ten))
        rep["tension_vs_closeness"] = round(cov / math.sqrt(sum((a - mc) ** 2 for a in close) * sum((b - mt) ** 2 for b in ten)), 3)
    json.dump(rep, open(os.path.join(out, "jev_report.json"), "w"), indent=1)
    L = ["# Jev on pro play", "", f"{rep['moments_scored']} of {rep['moments_planned']} moments from {rep['demos']} pro demos. Model {rep['model']}.", "",
         "| Predictor | Brier | AUC | ECE |", "| --- | --- | --- | --- |"]
    for k, v in rep["overall"].items():
        L.append(f"| {k} | {v['brier']} | {v['auc']} | {v['ece']} |")
    L += ["", f"Temperature fitted on pro: {best_t} (matchmaking: {t_mm}). Tension vs closeness: r = {rep['tension_vs_closeness']}.", "",
          "| Map | Moments | Jev (T matchmaking) | Logistic + economy |", "| --- | --- | --- | --- |"]
    L += [f"| {m} | {v['moments']} | {v['jev_T_matchmaking']} | {v['logistic_econ']} |" for m, v in rep["per_map"].items()]
    L += ["", "```", json.dumps(rep["ledger"], indent=1), "```"]
    open(os.path.join(out, "jev.md"), "w").write("\n".join(L) + "\n")
    print("\n".join(L))


if __name__ == "__main__":
    main()
