"""Model comparison on pro rounds, from the stored name-free facts.

M1  win probability on identical moments: Jev, gpt-oss-120b, Claude, and the
    logistic model trained on the other demos.
M2  one-sentence round recaps from both LLMs, checked by code against the
    facts and by Jev against the timeline.

Claude goes through AI Gateway with Unified Billing (Cloudflare token, no
Anthropic key); gpt-oss and Jev through Workers AI. Every model has a time box
and Claude a dollar cap. Tokens come from the environment and are never printed.

usage: models.py <dir with facts/> <plan.json> <config.json> <out dir>
"""
import json
import math
import os
import random
import re
import sys
import threading
import time
import urllib.request
from concurrent.futures import ThreadPoolExecutor

import anthropic

import analyze as A
import jev_pro as J

ACCOUNT = os.environ["CLOUDFLARE_ACCOUNT_ID"]
TOKEN = os.environ["CLOUDFLARE_JEV"]
WIN_PROMPT = ("Below is the state of a Counter-Strike 2 round at one moment, as JSON. Estimate the probability "
              "that the CT side wins this round from this moment, using players alive, economy, the bomb and the "
              "time left. Answer with JSON only: {\"p_ct\": <number between 0 and 1>}.\n\n")
RECAP_PROMPT = ("Below is a finished Counter-Strike 2 round as JSON: the timeline and the result. Write one factual "
                "sentence of at most 30 words recapping it for a spectator. Refer to players only by their labels "
                "(p0, p1, ...). State nothing the timeline does not show. Answer with JSON only: {\"recap\": \"...\"}.\n\n")
P_SCHEMA = {"type": "object", "properties": {"p_ct": {"type": "number"}}, "required": ["p_ct"], "additionalProperties": False}
R_SCHEMA = {"type": "object", "properties": {"recap": {"type": "string"}}, "required": ["recap"], "additionalProperties": False}
CHECK = {"unsupported": {"type": "noul", "instructions": "Does the recap state anything that the timeline and result do not support?",
                         "criteria": {"true": "The recap claims an event, player, count, site or outcome that the timeline does not show",
                                      "false": "Everything in the recap is shown by the timeline and result"}}}


class Ledger:
    def __init__(self):
        self.lock = threading.Lock()
        self.rows = {}

    def add(self, name, ms=None, tin=0, tout=0, error=False):
        with self.lock:
            r = self.rows.setdefault(name, {"calls": 0, "errors": 0, "in": 0, "out": 0, "ms": []})
            r["calls"] += 1
            r["errors"] += error
            r["in"] += tin
            r["out"] += tout
            if ms is not None:
                r["ms"].append(ms)

    def summary(self, prices):
        out = {}
        for k, r in self.rows.items():
            ms = sorted(r["ms"])
            usd = r["in"] * prices.get(k, (0, 0))[0] / 1e6 + r["out"] * prices.get(k, (0, 0))[1] / 1e6
            out[k] = {"calls": r["calls"], "errors": r["errors"], "input_tokens": r["in"], "output_tokens": r["out"],
                      "usd": round(usd, 4) if k in prices else None,
                      "p50_ms": ms[len(ms) // 2] if ms else None, "p95_ms": ms[int(len(ms) * 0.95)] if ms else None}
        return out


LEDGER = Ledger()
CLAUDE_PRICE = (4.0, 20.0)  # $ per 1M input / output tokens, claude-opus-5-5


class Claude:
    def __init__(self, model, max_usd):
        # The Cloudflare token goes out as Authorization: Bearer. An x-api-key of
        # any value is forwarded to Anthropic and rejected, so none is sent.
        self.client = anthropic.Anthropic(
            auth_token=TOKEN,
            base_url=f"https://gateway.ai.cloudflare.com/v1/{ACCOUNT}/default/anthropic",
            max_retries=4,
        )
        self.model, self.max_usd = model, max_usd
        self.first_error = None

    def spent(self):
        r = LEDGER.rows.get("claude", {"in": 0, "out": 0})
        return r["in"] * CLAUDE_PRICE[0] / 1e6 + r["out"] * CLAUDE_PRICE[1] / 1e6

    def json(self, prompt, schema):
        if self.spent() >= self.max_usd:
            raise RuntimeError("claude dollar cap")
        t0 = time.time()
        try:
            resp = self.client.messages.create(
                model=self.model, max_tokens=2048,
                output_config={"effort": "low", "format": {"type": "json_schema", "schema": schema}},
                messages=[{"role": "user", "content": prompt}],
            )
        except anthropic.APIStatusError as e:
            LEDGER.add("claude", error=True)
            self.first_error = self.first_error or f"{e.status_code}: {e.message}"[:200]
            raise
        except anthropic.APIConnectionError as e:
            LEDGER.add("claude", error=True)
            self.first_error = self.first_error or f"connection: {e}"[:200]
            raise
        LEDGER.add("claude", round((time.time() - t0) * 1000), resp.usage.input_tokens, resp.usage.output_tokens,
                   error=resp.stop_reason not in ("end_turn", "stop_sequence"))
        if resp.stop_reason not in ("end_turn", "stop_sequence"):
            raise RuntimeError(f"claude stop_reason {resp.stop_reason}")
        return json.loads(next(b.text for b in resp.content if b.type == "text"))


class Oss:
    def __init__(self, model):
        self.url = f"https://api.cloudflare.com/client/v4/accounts/{ACCOUNT}/ai/run/{model}"

    def json(self, prompt, key):
        body = json.dumps({"input": prompt, "reasoning": {"effort": "low"}}).encode()
        for attempt in range(4):
            t0 = time.time()
            try:
                req = urllib.request.Request(self.url, data=body, method="POST",
                                             headers={"Authorization": f"Bearer {TOKEN}", "Content-Type": "application/json"})
                with urllib.request.urlopen(req, timeout=120) as r:
                    res = json.load(r)["result"]
                text = "".join(c.get("text", "") for o in res.get("output", []) if o.get("type") == "message"
                               for c in o.get("content", []) if c.get("type") == "output_text")
                usage = res.get("usage") or {}
                LEDGER.add("gpt-oss", round((time.time() - t0) * 1000), usage.get("input_tokens", 0), usage.get("output_tokens", 0))
                m = re.search(r"\{.*\}", text, re.S)
                return json.loads(m.group(0))[key] if m else None
            except Exception:
                time.sleep(1 + 2 * attempt)
        LEDGER.add("gpt-oss", error=True)
        return None


def full_state(m, r):
    """The whole round with its result, as the recap models see it."""
    base = J.states(m, r)[-1][1]
    kills = [k for k in r.get("kills") or [] if not k.get("post")]
    lines = [(k["t"], J.kill_line(k)) for k in kills]
    if r.get("plant"):
        p = r["plant"]
        site = f" at bombsite {p['site']}" if p.get("site") else ""
        lines.append((p["t"] - 0.01, f"{J.clock(p['t'])} p{p['p']} (T) plants the bomb{site}."))
    if r.get("defuse"):
        d = r["defuse"]
        lines.append((d["t"], f"{J.clock(d['t'])} p{d['p']} (CT) defuses the bomb."))
    if r.get("explode") is not None:
        lines.append((r["explode"], f"{J.clock(r['explode'])} The bomb explodes."))
    st = {k: v for k, v in base.items() if k != "now"}
    st["timeline"] = [l for _, l in sorted(lines)]
    st["result"] = f"{r['winner']} win by {r['reason'].replace('_', ' ')}"
    return st


def code_check(recap, r):
    """Claims in a recap that the facts contradict. Each entry names the check."""
    errs = []
    text = recap.lower()
    kills = [k for k in r.get("kills") or [] if not k.get("post")]
    present = {k["k"] for k in kills if k.get("k", -1) >= 0} | {k["v"] for k in kills}
    if r.get("plant"):
        present.add(r["plant"]["p"])
    if r.get("defuse"):
        present.add(r["defuse"]["p"])
    for pid in {int(x) for x in re.findall(r"\bp(\d+)\b", text)}:
        if pid not in present:
            errs.append(f"player p{pid} not in the round")
    w, l = r["winner"].lower(), ("t" if r["winner"] == "CT" else "ct")
    if re.search(rf"\b{l}s?\b[^.]*\b(win|wins|won|take|takes|took|secure|secures|secured|close out|closes out)\b", text) and not re.search(rf"\b{w}s?\b[^.]*\b(win|wins|won|take|takes|took)", text):
        errs.append("names the losing side as winner")
    site = (r.get("plant") or {}).get("site")
    for s in set(re.findall(r"\b(?:bombsite |site )([ab])\b|\b([ab])[ -]site\b", text)):
        s = (s[0] or s[1]).upper()
        if site and s != site:
            errs.append(f"says site {s}, plant was at {site}")
        if not site:
            errs.append(f"mentions site {s} but there was no plant")
    if re.search(r"\bdefus(e|ed|es|ing)\b", text) and r["reason"] != "bomb_defused" and not r.get("defuse"):
        errs.append("claims a defuse")
    if re.search(r"\b(explode|explodes|exploded|detonat)", text) and r["reason"] != "bomb_exploded":
        errs.append("claims the bomb exploded")
    per = {}
    for k in kills:
        if k.get("k", -1) >= 0 and k.get("ks") and k.get("ks") != k.get("vs"):
            per[k["k"]] = per.get(k["k"], 0) + 1
    if re.search(r"\bace\b", text) and max(per.values(), default=0) < 5:
        errs.append("claims an ace")
    m = re.search(r"\b1v([2-5])\b", text)
    if m:
        need, ok = int(m.group(1)), False
        for k in kills:
            a = k.get("a") or [0, 0]
            for mine, theirs in ((a[0], a[1]), (a[1], a[0])):
                ok |= mine == 1 and theirs >= need
        if not ok:
            errs.append(f"claims a 1v{need}")
    return errs


def score(ps, ys):
    e, _ = J.A_ece(ps, ys)
    return {"n": len(ys), "brier": round(sum((p - y) ** 2 for p, y in zip(ps, ys)) / len(ys), 4), "auc": round(J.auc(ps, ys), 3), "ece": round(e, 3)}


def fit_t(ps, ys):
    return min((sum((J.cal(p, t) - y) ** 2 for p, y in zip(ps, ys)), t) for t in [0.2 + 0.02 * i for i in range(141)])[1]


def main():
    src, plan_path, cfg_path, out = sys.argv[1:5]
    plan, full_cfg = json.load(open(plan_path)), json.load(open(cfg_path))
    cfg, jcfg = full_cfg["models"], full_cfg["jev"]
    os.makedirs(out, exist_ok=True)
    demos, _ = A.load(src, plan)
    rng = random.Random(cfg["seed"])
    picked = sorted(demos, key=lambda d: d["id"])
    rng.shuffle(picked)
    sample = picked[: cfg["demos"]]
    sample_ids = {d["id"] for d in sample}

    moments, rounds = [], []
    for d in sample:
        for r in d["rounds"]:
            if r["n"] > 24:
                continue
            rounds.append((d["map_name"], r))
            kills = [k for k in r.get("kills") or [] if not k.get("post")]
            plant_t = (r.get("plant") or {}).get("t")
            for cut, st in J.states(d["map_name"], r):
                k = kills[cut] if cut >= 0 else None
                a = (k or {}).get("a") or [5, 5]
                moments.append({"map": d["map_name"], "y": 1 if r["winner"] == "CT" else 0, "ct": a[0], "t": a[1],
                                "planted": int(plant_t is not None and k is not None and plant_t <= k["t"]),
                                "sec": k["t"] if k else 0.0, "kind": "kill" if k else "start",
                                "equip_ct": (r.get("econ") or {}).get("CT", {}).get("equip", 0),
                                "equip_t": (r.get("econ") or {}).get("T", {}).get("equip", 0), "state": st})
    moments = rng.sample(moments, min(cfg["moments"], len(moments)))
    recap_rounds = rng.sample(rounds, min(cfg["recaps"], len(rounds)))

    claude, oss, jev = Claude(cfg["claude_model"], cfg["max_claude_usd"]), Oss(cfg["oss_model"]), J.Jev(jcfg["rps"], jcfg["max_input_tokens"])
    deadline = time.time() + cfg["minutes"] * 60

    def win(x):
        if time.time() > deadline:
            return None
        prompt = WIN_PROMPT + json.dumps(x["state"], separators=(",", ":"))
        res = {"y": x["y"], "map": x["map"]}
        try:
            res["jev"] = jev.ask(x["state"])["ct_wins"]["noul"]
        except Exception:
            pass
        try:
            res["claude"] = float(claude.json(prompt, P_SCHEMA)["p_ct"])
        except Exception:
            pass
        v = oss.json(prompt, "p_ct")
        if isinstance(v, (int, float)):
            res["gpt-oss"] = float(v)
        res["features"] = {k: x[k] for k in ("ct", "t", "planted", "sec", "equip_ct", "equip_t", "map")}
        return res

    def recap(item):
        if time.time() > deadline:
            return None
        m, r = item
        st = full_state(m, r)
        prompt = RECAP_PROMPT + json.dumps(st, separators=(",", ":"))
        out_r = {"map": m}
        for name, fn in (("claude", lambda: claude.json(prompt, R_SCHEMA)["recap"]), ("gpt-oss", lambda: oss.json(prompt, "recap"))):
            try:
                text = fn()
            except Exception:
                text = None
            if text:
                out_r[name] = {"recap": text, "code_errors": code_check(text, r)}
        return out_r

    with ThreadPoolExecutor(cfg["workers"]) as ex:
        wins = [w for w in ex.map(win, moments) if w]
        recaps = [r for r in ex.map(recap, recap_rounds) if r]

    # Jev as fact-checker on every recap, with the recap inside the state.
    def jev_check(text, st):
        body = json.dumps({"model": "typesafe/jev", "input": {"state": dict(st, recap=text), "questions": CHECK}}).encode()
        req = urllib.request.Request(jev.url, data=body, method="POST", headers={"Authorization": f"Bearer {TOKEN}", "Content-Type": "application/json"})
        with urllib.request.urlopen(req, timeout=60) as r:
            return (json.load(r).get("result") or {}).get("result", {}).get("answers", {}).get("unsupported", {}).get("noul")
    for item, rec in zip(recap_rounds, recaps):
        st = full_state(*item)
        for name in ("claude", "gpt-oss"):
            if name in rec:
                try:
                    rec[name]["jev_unsupported"] = jev_check(rec[name]["recap"], st)
                except Exception:
                    rec[name]["jev_unsupported"] = None

    # Logistic baseline trained on every demo outside the sample.
    train = A.moments(A.round_rows([d for d in demos if d["id"] not in sample_ids]))
    maps = sorted({x["map"] for x in train})
    w = A.fit([A.feats(x, "econ", maps) for x in train], [x["y"] for x in train])
    for x in wins:
        x["logistic"] = A.predict(w, A.feats(x["features"], "econ", maps))

    # Score on the moments every model that answered most of them shares, so a
    # model that failed outright can't empty the comparison.
    answered = {n: sum(1 for x in wins if n in x) for n in ("jev", "claude", "gpt-oss", "logistic")}
    names = [n for n, c in answered.items() if c >= 0.5 * len(moments)]
    common = [x for x in wins if all(n in x for n in names)]
    ys = [x["y"] for x in common]
    m1 = {"moments_asked": len(moments), "answered": answered, "compared": names, "moments_compared": len(common),
          "claude_first_error": claude.first_error, "by_model": {}}
    for n in names:
        ps = [x[n] for x in common]
        if not ps or len(set(ys)) < 2:
            continue
        t = fit_t(ps, ys) if n != "logistic" else 1.0
        m1["by_model"][n] = {"raw": score(ps, ys), "temperature": t, "recalibrated": score([J.cal(p, t) for p in ps], ys),
                             "answered": answered[n]}
    m2 = {"rounds": len(recaps), "by_model": {}}
    for n in ("claude", "gpt-oss"):
        rs = [r[n] for r in recaps if n in r]
        if not rs:
            continue
        flagged = [r for r in rs if r.get("jev_unsupported") is not None and r["jev_unsupported"] > 0.5]
        m2["by_model"][n] = {"recaps": len(rs), "with_code_errors": sum(1 for r in rs if r["code_errors"]),
                             "code_error_kinds": sorted({e.split(" p")[0] if e.startswith("player") else e for r in rs for e in r["code_errors"]}),
                             "jev_flagged": len(flagged),
                             "both_flag": sum(1 for r in rs if r["code_errors"] and r.get("jev_unsupported") and r["jev_unsupported"] > 0.5),
                             "examples_ok": [r["recap"] for r in rs if not r["code_errors"] and (r.get("jev_unsupported") or 0) <= 0.5][:3],
                             "examples_flagged": [{"recap": r["recap"], "code_errors": r["code_errors"], "jev": r.get("jev_unsupported")} for r in rs if r["code_errors"] or (r.get("jev_unsupported") or 0) > 0.5][:5]}
    rep = {"m1_win_probability": m1, "m2_recaps": m2,
           "ledger": LEDGER.summary({"claude": CLAUDE_PRICE}), "jev_ledger": {"calls": jev.calls, "errors": jev.errors, "input_tokens": jev.tokens, "usd": round(jev.tokens * 0.042e-6, 4)},
           "models": {"claude": cfg["claude_model"], "gpt-oss": cfg["oss_model"], "jev": jev.model}}
    json.dump(rep, open(os.path.join(out, "models_report.json"), "w"), indent=1)
    L = ["# Model comparison on pro rounds", "", f"M1: {len(common)} of {len(moments)} moments answered by every compared model ({', '.join(names)}). Answered: {answered}." +
         (f" Claude's first error: {claude.first_error}" if claude.first_error else ""), "",
         "| Model | Brier | AUC | ECE | Fitted T | Brier recalibrated |", "| --- | --- | --- | --- | --- | --- |"]
    for n, v in m1["by_model"].items():
        L.append(f"| {n} | {v['raw']['brier']} | {v['raw']['auc']} | {v['raw']['ece']} | {v['temperature']} | {v['recalibrated']['brier']} |")
    L += ["", f"M2: {m2['rounds']} rounds recapped.", "", "| Model | Recaps | With code-detected errors | Flagged by Jev | Both |", "| --- | --- | --- | --- | --- |"]
    for n, v in m2["by_model"].items():
        L.append(f"| {n} | {v['recaps']} | {v['with_code_errors']} | {v['jev_flagged']} | {v['both_flag']} |")
    L += ["", "```", json.dumps({"ledger": rep["ledger"], "jev": rep["jev_ledger"]}, indent=1), "```"]
    open(os.path.join(out, "models.md"), "w").write("\n".join(L) + "\n")
    print("\n".join(L))


if __name__ == "__main__":
    main()
