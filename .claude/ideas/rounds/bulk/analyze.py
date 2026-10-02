"""Round-dynamics study over the bulk facts: what decides rounds, how it
differs by map, which situations are balanced and which are not, and how
predictable each map is. Also summarises how our parser did on every demo.
Standard library only. Writes report.json and report.md.

usage: analyze.py <dir with facts/ and parser/> <plan.json> <out dir>
"""
import glob
import json
import math
import os
import random
import sys
import traceback
from collections import Counter, defaultdict

ROUND_TIME = 115
MIN_N = 40  # smallest sample a cell needs before it is ranked or compared


# ---------- helpers ----------

def wilson(k, n, z=1.96):
    if n == 0:
        return None
    p = k / n
    d = 1 + z * z / n
    c = (p + z * z / (2 * n)) / d
    h = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n)) / d
    return [round(p, 3), round(c - h, 3), round(c + h, 3)]


def rate(pairs):
    """pairs: list of 0/1 outcomes -> {n, p, lo, hi}"""
    n = len(pairs)
    if n == 0:
        return {"n": 0}
    w = wilson(sum(pairs), n)
    return {"n": n, "p": w[0], "lo": w[1], "hi": w[2]}


def other(side):
    return "T" if side == "CT" else "CT"


def buy(total, pistol):
    if pistol:
        return "pistol"
    if total < 5000:
        return "eco"
    if total < 10000:
        return "semi"
    if total < 20000:
        return "force"
    return "full"


def pct(x):
    return f"{100 * x:.0f}%"


# ---------- load ----------

def load(src, plan):
    maps = {it["path"]: it["map"] for it in plan["items"]}
    recs = {}
    for f in glob.glob(os.path.join(src, "parser", "*.jsonl")):
        for line in open(f):
            if line.strip():
                r = json.loads(line)
                recs[r["id"]] = r
    demos = []
    for f in sorted(glob.glob(os.path.join(src, "facts", "*.json"))):
        id_ = os.path.basename(f)[:-5]
        try:
            d = json.load(open(f))
        except Exception:
            continue
        if not d.get("rounds"):
            continue
        rec = recs.get(id_, {})
        d["id"] = id_
        d["map_name"] = maps.get(rec.get("path"), (d.get("map") or "").replace("de_", "")) or "unknown"
        demos.append(d)
    return demos, list(recs.values())


def round_rows(demos):
    rows = []
    for d in demos:
        rounds = d["rounds"]
        for i, r in enumerate(rounds):
            n = r["n"]
            econ = r.get("econ") or {}
            kills = [k for k in r.get("kills") or [] if not k.get("post")]
            first = kills[0] if kills else None
            opener = None
            if first:
                ks, vs = first.get("ks"), first.get("vs")
                opener = ks if ks and ks != vs else (other(vs) if vs else None)
            plant = r.get("plant")
            rows.append({
                "demo": d["id"], "map": d["map_name"], "n": n, "reg": n <= 24, "pistol": n in (1, 13),
                "winner": r["winner"], "reason": r["reason"],
                "ct_is_a": r.get("ct_is_a"), "score_ct": r.get("score_ct", 0), "score_t": r.get("score_t", 0),
                "buy_ct": buy(econ.get("CT", {}).get("equip", 0), n in (1, 13)),
                "buy_t": buy(econ.get("T", {}).get("equip", 0), n in (1, 13)),
                "equip_ct": econ.get("CT", {}).get("equip", 0), "equip_t": econ.get("T", {}).get("equip", 0),
                "awps_ct": econ.get("CT", {}).get("awps", 0), "awps_t": econ.get("T", {}).get("awps", 0),
                "kills": kills, "opener": opener, "first": first, "plant": plant,
                "defuse": r.get("defuse"), "nades": r.get("nades") or {}, "blind": r.get("blind_s") or {},
                "dmg": r.get("dmg") or {},
                "prev_winner": rounds[i - 1]["winner"] if i > 0 and rounds[i - 1]["n"] == n - 1 else None,
                "prev_pistol": i > 0 and rounds[i - 1]["n"] in (1, 13) and n in (2, 14),
            })
    return rows


def moments(rows):
    """One row per decision point: round start and after every in-round kill."""
    out = []
    for r in rows:
        y = 1 if r["winner"] == "CT" else 0
        pt = r["plant"]["t"] if r["plant"] else None
        rid = f"{r['demo']}:{r['n']}"
        out.append({"map": r["map"], "demo": r["demo"], "rid": rid, "y": y, "ct": 5, "t": 5, "planted": 0, "sec": 0.0,
                    "equip_ct": r["equip_ct"], "equip_t": r["equip_t"], "kind": "start"})
        for k in r["kills"]:
            a = k.get("a") or [0, 0]
            out.append({"map": r["map"], "demo": r["demo"], "rid": rid, "y": y, "ct": a[0], "t": a[1],
                        "planted": int(pt is not None and pt <= k["t"]), "sec": k["t"],
                        "equip_ct": r["equip_ct"], "equip_t": r["equip_t"], "kind": "kill"})
    return out


# ---------- analyses ----------

def map_balance(rows):
    res = {}
    for m in sorted({r["map"] for r in rows}) + ["ALL"]:
        rs = [r for r in rows if r["reg"] and (m == "ALL" or r["map"] == m)]
        if not rs:
            continue
        ct = [int(r["winner"] == "CT") for r in rs]
        pis = [int(r["winner"] == "CT") for r in rs if r["pistol"]]
        reasons = Counter(r["reason"] for r in rs)
        res[m] = {"rounds": len(rs), "demos": len({r["demo"] for r in rs}), "ct_win": rate(ct), "pistol_ct_win": rate(pis),
                  "reasons": {k: round(v / len(rs), 3) for k, v in reasons.most_common()},
                  "plant_rate": round(sum(1 for r in rs if r["plant"]) / len(rs), 3)}
    return res


def opening(rows):
    res = {}
    for m in sorted({r["map"] for r in rows}) + ["ALL"]:
        rs = [r for r in rows if r["opener"] and (m == "ALL" or r["map"] == m)]
        cell = {}
        for s in ("CT", "T"):
            cell[f"{s}_opens"] = rate([int(r["winner"] == s) for r in rs if r["opener"] == s])
        cell["opener_wins"] = rate([int(r["winner"] == r["opener"]) for r in rs])
        res[m] = cell
    return res


def man_advantage(moms):
    res = {}
    states = [(5, 4), (4, 5), (4, 4), (4, 3), (3, 4), (3, 3), (3, 2), (2, 3), (2, 2), (2, 1), (1, 2), (1, 1), (5, 3), (3, 5)]
    for m in sorted({x["map"] for x in moms}) + ["ALL"]:
        ms = [x for x in moms if x["kind"] == "kill" and (m == "ALL" or x["map"] == m)]
        cell = {}
        for ct, t in states:
            for planted in (0, 1):
                ys = [x["y"] for x in ms if x["ct"] == ct and x["t"] == t and x["planted"] == planted]
                if ys:
                    cell[f"{ct}v{t}{'p' if planted else ''}"] = rate(ys)
        res[m] = cell
    return res


def post_plant(rows):
    res = {}
    for m in sorted({r["map"] for r in rows}) + ["ALL"]:
        rs = [r for r in rows if r["plant"] and (m == "ALL" or r["map"] == m)]
        cell = {"plants": len(rs), "t_win": rate([int(r["winner"] == "T") for r in rs])}
        for site in ("A", "B"):
            cell[f"site_{site}"] = rate([int(r["winner"] == "T") for r in rs if r["plant"].get("site") == site])
        by_adv = defaultdict(list)
        for r in rs:
            a = r["plant"].get("a") or [0, 0]
            by_adv[max(-3, min(3, a[1] - a[0]))].append(int(r["winner"] == "T"))
        cell["t_win_by_t_minus_ct_alive_at_plant"] = {str(k): rate(v) for k, v in sorted(by_adv.items())}
        defs = [r for r in rs if r["winner"] == "CT" and r["reason"] == "bomb_defused"]
        cell["defuse_with_kit"] = round(sum(1 for r in defs if (r["defuse"] or {}).get("kit")) / len(defs), 3) if defs else None
        cell["ninja_defuses"] = sum(1 for r in defs if ((r["defuse"] or {}).get("a") or [0, 0])[1] > 0)
        cell["mean_plant_s"] = round(sum(r["plant"]["t"] for r in rs) / len(rs), 1) if rs else None
        res[m] = cell
    return res


def economy(rows):
    res = {}
    classes = ["eco", "semi", "force", "full"]
    for m in sorted({r["map"] for r in rows}) + ["ALL"]:
        rs = [r for r in rows if r["reg"] and not r["pistol"] and (m == "ALL" or r["map"] == m)]
        grid = {}
        for a in classes:
            for b in classes:
                ys = [int(r["winner"] == "CT") for r in rs if r["buy_ct"] == a and r["buy_t"] == b]
                if ys:
                    grid[f"CT {a} v T {b}"] = rate(ys)
        upset = []
        for r in rs:
            for s in ("CT", "T"):
                mine, theirs = r["buy_" + s.lower()], r["buy_" + other(s).lower()]
                if mine in ("eco", "semi") and theirs == "full":
                    upset.append(int(r["winner"] == s))
        force = []
        for r in rs:
            for s in ("CT", "T"):
                if r["buy_" + s.lower()] == "force" and r["buy_" + other(s).lower()] == "full":
                    force.append(int(r["winner"] == s))
        conv = [int(r["winner"] == r["prev_winner"]) for r in rows if r["prev_pistol"] and r["prev_winner"] and (m == "ALL" or r["map"] == m)]
        res[m] = {"matchups_ct_win": grid, "eco_beats_full": rate(upset), "force_beats_full": rate(force),
                  "pistol_winner_wins_next": rate(conv)}
    return res


def events(rows):
    """Win rate for the side an event favours, per map. The furthest from 50%
    are the least balanced; those near 50% are coin flips."""
    defs = {
        "opening kill (CT)": lambda r: ("CT", r["opener"] == "CT") if r["opener"] else None,
        "opening kill (T)": lambda r: ("T", r["opener"] == "T") if r["opener"] else None,
        "opening kill with an AWP": lambda r: (r["opener"], (r["first"] or {}).get("w") == "AWP") if r["opener"] else None,
        "opening kill traded within 5 s": lambda r: (other(r["opener"]), traded(r)) if r["opener"] else None,
        "flash-assisted opening kill": lambda r: (r["opener"], bool((r["first"] or {}).get("fa"))) if r["opener"] else None,
        "bomb planted": lambda r: ("T", bool(r["plant"])),
        "plant at A": lambda r: ("T", (r["plant"] or {}).get("site") == "A") if r["plant"] else None,
        "plant at B": lambda r: ("T", (r["plant"] or {}).get("site") == "B") if r["plant"] else None,
        "plant with a man advantage": lambda r: ("T", adv_at_plant(r) > 0) if r["plant"] else None,
        "plant while down players": lambda r: ("T", adv_at_plant(r) < 0) if r["plant"] else None,
        "CT has more AWPs": lambda r: ("CT", r["awps_ct"] > r["awps_t"]),
        "T has more AWPs": lambda r: ("T", r["awps_t"] > r["awps_ct"]),
        "more smokes thrown (CT)": lambda r: ("CT", nade(r, "CT", "smoke") > nade(r, "T", "smoke")),
        "more smokes thrown (T)": lambda r: ("T", nade(r, "T", "smoke") > nade(r, "CT", "smoke")),
        "more enemy blind time (CT)": lambda r: ("CT", (r["blind"].get("CT", 0) or 0) > (r["blind"].get("T", 0) or 0) + 5),
        "more enemy blind time (T)": lambda r: ("T", (r["blind"].get("T", 0) or 0) > (r["blind"].get("CT", 0) or 0) + 5),
        "eco vs full buy (eco side)": lambda r: eco_side(r),
        "pistol round (CT)": lambda r: ("CT", r["pistol"]),
        "won the previous round (CT)": lambda r: ("CT", r["prev_winner"] == "CT") if r["prev_winner"] and not r["pistol"] else None,
        "won the previous round (T)": lambda r: ("T", r["prev_winner"] == "T") if r["prev_winner"] and not r["pistol"] else None,
    }
    maps = sorted({r["map"] for r in rows})
    out = {}
    for name, fn in defs.items():
        per = {}
        for m in maps + ["ALL"]:
            ys = []
            for r in rows:
                if not r["reg"] or (m != "ALL" and r["map"] != m):
                    continue
                v = fn(r)
                if v and v[0] and v[1]:
                    ys.append(int(r["winner"] == v[0]))
            per[m] = rate(ys)
        ok = [per[m]["p"] for m in maps if per[m]["n"] >= MIN_N]
        out[name] = {"by_map": per, "map_spread": round(max(ok) - min(ok), 3) if len(ok) >= 2 else None}
    return out


def opening_places(rows):
    """Where each map's first duel happens (the victim's callout), and how
    often the side that won it took the round."""
    res = {}
    for m in sorted({r["map"] for r in rows}):
        rs = [r for r in rows if r["map"] == m and r["first"] and r["opener"]]
        by = defaultdict(list)
        for r in rs:
            by[(r["first"].get("vp") or "?", r["opener"])].append(int(r["winner"] == r["opener"]))
        spots = [{"place": p, "opener": s, "share": round(len(v) / len(rs), 3), **rate(v)}
                 for (p, s), v in by.items() if len(v) >= 12]
        res[m] = {"openings": len(rs), "spots": sorted(spots, key=lambda c: -c["n"])[:12]}
    return res


def plant_timing(rows):
    buckets = (("early (<0:45)", 0, 45), ("mid (0:45–1:15)", 45, 75), ("late (>1:15)", 75, 1e9))
    res = {}
    for m in sorted({r["map"] for r in rows}) + ["ALL"]:
        rs = [r for r in rows if r["plant"] and (m == "ALL" or r["map"] == m)]
        res[m] = {name: rate([int(r["winner"] == "T") for r in rs if lo <= r["plant"]["t"] < hi]) for name, lo, hi in buckets}
    return res


def clutches(moms):
    """Each side's first 1vN of a round, counted once, and how often the lone
    player's side won it."""
    first = {}
    for x in moms:
        if x["kind"] != "kill":
            continue
        for side, mine, theirs in (("CT", x["ct"], x["t"]), ("T", x["t"], x["ct"])):
            key = (x["rid"], side)
            if mine == 1 and theirs >= 1 and key not in first:
                won = x["y"] if side == "CT" else 1 - x["y"]
                first[key] = (x["map"], side, min(theirs, 3), x["planted"], won)
    res = {}
    for m in sorted({v[0] for v in first.values()}) + ["ALL"]:
        cell = {}
        for n in (1, 2, 3):
            label = f"1v{n}" + ("+" if n == 3 else "")
            for side in ("CT", "T"):
                cell[f"{side} {label}"] = rate([v[4] for v in first.values() if (m == "ALL" or v[0] == m) and v[1] == side and v[2] == n])
        res[m] = cell
    return res


def traded(r):
    ks = r["kills"]
    if len(ks) < 2:
        return False
    f = ks[0]
    return any(k["t"] - f["t"] <= 5 and k.get("v") == f.get("k") for k in ks[1:])


def adv_at_plant(r):
    a = r["plant"].get("a") or [0, 0]
    return a[1] - a[0]


def nade(r, s, kind):
    return (r["nades"].get(s) or {}).get(kind, 0)


def eco_side(r):
    if r["pistol"]:
        return None
    for s in ("CT", "T"):
        if r["buy_" + s.lower()] in ("eco", "semi") and r["buy_" + other(s).lower()] == "full":
            return (s, True)
    return None


# ---------- predictability ----------

def feats(x, variant, maps):
    left = max(0.0, ROUND_TIME - x["sec"]) / ROUND_TIME
    f = [1.0, x["ct"] - x["t"], x["ct"], x["t"], x["planted"], x["planted"] * x["ct"], left]
    if variant in ("econ", "econ+map"):
        f += [x["equip_ct"] / 10000, x["equip_t"] / 10000]
    if variant in ("map", "econ+map"):
        f += [1.0 if x["map"] == m else 0.0 for m in maps[1:]]
    return f


def solve(A, b):
    n = len(b)
    M = [row[:] + [b[i]] for i, row in enumerate(A)]
    for c in range(n):
        p = max(range(c, n), key=lambda r: abs(M[r][c]))
        M[c], M[p] = M[p], M[c]
        if abs(M[c][c]) < 1e-12:
            continue
        for r in range(n):
            if r != c:
                f = M[r][c] / M[c][c]
                for k in range(c, n + 1):
                    M[r][k] -= f * M[c][k]
    return [M[i][n] / M[i][i] if abs(M[i][i]) > 1e-12 else 0.0 for i in range(n)]


def fit(X, y, ridge=1e-3, iters=8):
    k = len(X[0])
    w = [0.0] * k
    for _ in range(iters):
        H = [[ridge if i == j else 0.0 for j in range(k)] for i in range(k)]
        g = [-ridge * wi for wi in w]
        for x, t in zip(X, y):
            z = sum(a * b for a, b in zip(w, x))
            p = 1 / (1 + math.exp(-max(-30, min(30, z))))
            s = p * (1 - p)
            for i in range(k):
                g[i] += (t - p) * x[i]
                xi = s * x[i]
                for j in range(i, k):
                    H[i][j] += xi * x[j]
        for i in range(k):
            for j in range(i):
                H[i][j] = H[j][i]
        step = solve(H, g)
        w = [a + b for a, b in zip(w, step)]
    return w


def predict(w, x):
    z = sum(a * b for a, b in zip(w, x))
    return 1 / (1 + math.exp(-max(-30, min(30, z))))


def predictability(moms, folds=5):
    maps = sorted({x["map"] for x in moms})
    demos = sorted({x["demo"] for x in moms})
    rng = random.Random(7)
    rng.shuffle(demos)
    fold = {d: i % folds for i, d in enumerate(demos)}
    res = {}
    preds = {}
    for variant in ("base", "map", "econ", "econ+map"):
        p = [0.0] * len(moms)
        for f in range(folds):
            tr = [i for i, x in enumerate(moms) if fold[x["demo"]] != f]
            te = [i for i, x in enumerate(moms) if fold[x["demo"]] == f]
            w = fit([feats(moms[i], variant, maps) for i in tr], [moms[i]["y"] for i in tr])
            for i in te:
                p[i] = predict(w, feats(moms[i], variant, maps))
        preds[variant] = p
        res[variant] = {"brier_all": round(sum((p[i] - x["y"]) ** 2 for i, x in enumerate(moms)) / len(moms), 4)}
    per_map = {}
    for m in maps:
        idx = [i for i, x in enumerate(moms) if x["map"] == m]
        start = [i for i in idx if moms[i]["kind"] == "start"]
        per_map[m] = {"moments": len(idx)}
        for v in preds:
            per_map[m][v] = round(sum((preds[v][i] - moms[i]["y"]) ** 2 for i in idx) / len(idx), 4)
        if start:
            per_map[m]["round_start_brier_econ"] = round(sum((preds["econ"][i] - moms[i]["y"]) ** 2 for i in start) / len(start), 4)
    res["per_map"] = per_map
    res["moments"] = len(moms)
    return res


# ---------- parser robustness ----------

REFERENCE_SIDE = {}  # demo file name -> why the difference is the reference's, filled from plan.json


def robustness(recs):
    out = {"demos": len(recs), "download_failed": sum(1 for r in recs if r.get("download") == "failed")}
    for step in ("facts", "oracle", "parse", "diff"):
        rs = [r[step] for r in recs if step in r]
        secs = sorted(x["s"] for x in rs if "s" in x)
        rss = sorted(x["rss_mb"] for x in rs if "rss_mb" in x)
        q = lambda v, p: v[min(len(v) - 1, int(p * (len(v) - 1)))] if v else None
        out[step] = {"runs": len(rs), "exit_codes": dict(Counter(str(x["rc"]) for x in rs)),
                     "s_p50": q(secs, .5), "s_p95": q(secs, .95), "s_max": q(secs, 1),
                     "rss_mb_p50": q(rss, .5), "rss_mb_p95": q(rss, .95), "rss_mb_max": q(rss, 1)}
    out["parse_failures"] = [{"path": r["path"], "map": r["map"], "rc": r["parse"]["rc"], "err": r["parse"].get("err")}
                             for r in recs if "parse" in r and r["parse"]["rc"] != 0]
    out["diff_mismatches"] = [{"path": r["path"], "map": r["map"], "rc": r["diff"]["rc"], "lines": r["diff"].get("report_lines")}
                              for r in recs if "diff" in r and r["diff"]["rc"] != 0]
    out["over_budget"] = [{"path": r["path"], "s": r["parse"].get("s"), "rss_mb": r["parse"].get("rss_mb")}
                          for r in recs if "parse" in r and (r["parse"].get("rss_mb", 0) > 512 or r["parse"].get("s", 0) > 60)]
    out["builds"] = dict(Counter(str((r.get("facts") or {}).get("build")) for r in recs))
    cats, by_build, examples, kinds = Counter(), defaultdict(Counter), defaultdict(list), Counter()
    for r in recs:
        c = category(r)
        cats[c] += 1
        by_build[str((r.get("facts") or {}).get("build"))][c] += 1
        if len(examples[c]) < 6:
            examples[c].append(r["path"])
        kinds.update((r.get("diff") or {}).get("kinds", {}).keys())
    out["categories"] = dict(cats.most_common())
    out["categories_by_build"] = {b: dict(v) for b, v in sorted(by_build.items())}
    out["category_examples"] = dict(examples)
    out["diff_kinds_demos"] = dict(kinds.most_common(30))
    out["round_files_differ"] = sum(1 for r in recs if any(k.startswith("round[]") for k in ((r.get("diff") or {}).get("kinds") or {})))
    return out


def category(r):
    """One outcome per demo, judged on match.json first. Round-file
    differences are counted separately (see round_files_differ)."""
    parse, diff = r.get("parse") or {}, r.get("diff") or {}
    if r.get("download") == "failed":
        return "download failed"
    if os.path.basename(r["path"]) in REFERENCE_SIDE and parse.get("rc") == 0:
        return "reference-side difference (known)"
    if parse.get("rc") == 0 and "diff" not in r:
        return "no reference (oracle failed)"
    if parse.get("rc") not in (0, None):
        try:
            code = json.loads(parse.get("err") or "{}").get("code")
        except ValueError:
            code = None
        return f"parse failed: {code or 'exit ' + str(parse.get('rc'))}"
    if diff.get("rc") == 0:
        return "diff OK"
    if "rounds" in diff:
        ours, ref = diff["rounds"]
        return "match: rounds collapsed" if ours <= 3 and ref >= 10 else "match: round count differs"
    match = [k for k in diff.get("kinds", {}) if not k.startswith("round[]")]
    if not match:
        return "match OK, round files differ"
    if "players[].team" in match:
        return "match: team assignment differs"
    if all(k.startswith("scoreboard") for k in match):
        return "match: scoreboard only"
    if all(k.startswith("rounds[]") for k in match):
        return "match: round boundary or reason differs"
    return "match: other"


# ---------- report ----------

def markdown(rep):
    L = ["# Round dynamics: pro demo study", "",
         f"{rep['demos']} demos, {rep['rounds']} rounds, {rep['moments']} decision moments. Dataset {rep['dataset']} at {rep['revision'][:10]}.", ""]
    mb = rep.get("map_balance", {})
    if mb:
        L += ["## Map balance (regulation rounds)", "", "| Map | Demos | Rounds | CT win | 95% CI | Pistol CT win | Plant rate |", "| --- | --- | --- | --- | --- | --- | --- |"]
        for m, c in sorted(mb.items(), key=lambda kv: -(kv[1]["ct_win"].get("p") or 0)):
            w, pw = c["ct_win"], c["pistol_ct_win"]
            L.append(f"| {m} | {c['demos']} | {c['rounds']} | {pct(w['p'])} | {pct(w['lo'])}–{pct(w['hi'])} | {pct(pw['p']) if pw.get('n') else '—'} (n={pw.get('n', 0)}) | {pct(c['plant_rate'])} |")
        L.append("")
    ev = rep.get("events", {})
    if ev:
        ranked = sorted(((k, v["by_map"]["ALL"]) for k, v in ev.items() if v["by_map"]["ALL"].get("n", 0) >= MIN_N), key=lambda kv: -abs(kv[1]["p"] - 0.5))
        L += ["## Events, least to most balanced (win rate for the side the event favours, all maps)", "", "| Event | n | Win rate | 95% CI | Spread across maps |", "| --- | --- | --- | --- | --- |"]
        for k, c in ranked:
            sp = ev[k]["map_spread"]
            L.append(f"| {k} | {c['n']} | {pct(c['p'])} | {pct(c['lo'])}–{pct(c['hi'])} | {pct(sp) if sp is not None else '—'} |")
        L.append("")
    pr = rep.get("predictability", {})
    if pr.get("per_map"):
        L += ["## Predictability (5-fold by demo, Brier, lower is better)", "",
              "Overall: " + ", ".join(f"{v} {pr[v]['brier_all']}" for v in ("base", "map", "econ", "econ+map") if v in pr), "",
              "| Map | Moments | Base | + map | + economy | Round-start Brier (economy) |", "| --- | --- | --- | --- | --- | --- |"]
        for m, c in sorted(pr["per_map"].items(), key=lambda kv: kv[1]["base"]):
            L.append(f"| {m} | {c['moments']} | {c['base']} | {c['map']} | {c['econ']} | {c.get('round_start_brier_econ', '—')} |")
        L.append("")
    cl = rep.get("clutches", {}).get("ALL")
    if cl:
        L += ["## Clutches (each side's first 1vN of a round, all maps)", "", "| Situation | n | Lone side wins |", "| --- | --- | --- |"]
        L += [f"| {k} | {v['n']} | {pct(v['p'])} |" for k, v in cl.items() if v.get("n")]
        L.append("")
    pt = rep.get("plant_timing", {}).get("ALL")
    if pt:
        L += ["## Plant timing (T win after the plant, all maps)", ""] + [f"- {k}: {pct(v['p'])} (n={v['n']})" for k, v in pt.items() if v.get("n")] + [""]
    op = rep.get("opening_places", {})
    if op:
        L += ["## Where the first duel happens (top spots per map; win rate for the side that won it)", ""]
        for m, c in sorted(op.items()):
            spots = ", ".join(f"{s['place']} by {s['opener']} {pct(s['share'])} of openings, wins {pct(s['p'])}" for s in c["spots"][:4])
            L.append(f"- **{m}** ({c['openings']}): {spots}")
        L.append("")
    rb = rep.get("robustness", {})
    if rb:
        L += ["## Our parser on these demos", "", f"Parser ref: {rep.get('parser_ref') or 'branch base'}.", "",
              "| Outcome | Demos |", "| --- | --- |"] + [f"| {k} | {v} |" for k, v in rb.get("categories", {}).items()] + [""]
        L += ["```", json.dumps({k: rb[k] for k in ("demos", "download_failed", "parse", "diff", "categories_by_build", "diff_kinds_demos") if k in rb}, indent=1), "```", "",
              f"Over budget (> 512 MB or > 60 s): {len(rb.get('over_budget', []))}.", ""]
    if rep.get("errors"):
        L += ["## Analysis errors", ""] + [f"- {e}" for e in rep["errors"]]
    return "\n".join(L) + "\n"


def main():
    src, plan_path, out = sys.argv[1:4]
    parser_only = "--parser-only" in sys.argv[4:]
    plan = json.load(open(plan_path))
    REFERENCE_SIDE.update(plan.get("reference_side") or {})
    os.makedirs(out, exist_ok=True)
    demos, recs = load(src, plan)
    rows = [] if parser_only else round_rows(demos)
    moms = moments(rows)
    rep = {"dataset": plan["dataset"], "revision": plan["revision"], "parser_ref": plan.get("parser_ref"),
           "demos": len(demos) if not parser_only else len(recs), "rounds": len(rows), "moments": len(moms), "errors": []}
    steps = [("robustness", robustness, recs)]
    if not parser_only:
        steps = [("map_balance", map_balance, rows), ("opening", opening, rows), ("opening_places", opening_places, rows),
                 ("man_advantage", man_advantage, moms), ("clutches", clutches, moms), ("post_plant", post_plant, rows),
                 ("plant_timing", plant_timing, rows), ("economy", economy, rows), ("events", events, rows),
                 ("predictability", predictability, moms)] + steps
    for name, fn, arg in steps:
        try:
            rep[name] = fn(arg)
        except Exception:
            rep["errors"].append(f"{name}: {traceback.format_exc(limit=3)}")
    json.dump(rep, open(os.path.join(out, "report.json"), "w"), indent=1)
    open(os.path.join(out, "report.md"), "w").write(markdown(rep))
    print(markdown(rep))


if __name__ == "__main__":
    main()
