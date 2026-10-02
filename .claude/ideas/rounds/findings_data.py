"""Turn the study's report.json (and the model comparison's models_report.json)
into the viewer's findings data file, web/src/findings/data.json.

    python findings_data.py report.json models_report.json out.json

Only aggregates are copied, each picked by name. The report also lists demo
paths, which carry team names, so nothing is copied wholesale.
"""

import json
import re
import sys

MAPS = ["ancient", "anubis", "dust2", "inferno", "mirage", "nuke", "overpass", "train", "vertigo"]
BUYS = ["full", "force", "semi", "eco"]
SPOTS_PER_MAP = 6
MIN_CELL = 30  # buy-matchup cells with fewer rounds are left out


def rate(x):
    return {"n": x["n"], "p": round(x["p"], 3), "lo": round(x["lo"], 3), "hi": round(x["hi"], 3)}


def place(name):
    s = re.sub(r"(?<=[a-z])(?=[A-Z])|(?<=[A-Z])(?=[A-Z][a-z])", " ", name)
    return re.sub(r"(?<=\w)of\b", " of", s)


def build(r, m):
    bal = r["map_balance"]
    out = {
        "demos": r["demos"],
        "rounds": bal["ALL"]["rounds"],
        "moments": r["moments"],
        "ct_win": rate(bal["ALL"]["ct_win"]),
        "pistol_ct_win": rate(bal["ALL"]["pistol_ct_win"]),
        "maps": sorted(
            ({"map": k, "demos": bal[k]["demos"], **rate(bal[k]["ct_win"])} for k in MAPS),
            key=lambda x: -x["p"],
        ),
    }

    pp = r["post_plant"]["ALL"]
    out["post_plant"] = {
        "plants": pp["plants"],
        "t_win": rate(pp["t_win"]),
        "by_alive_diff": [
            {"diff": int(k), **rate(v)}
            for k, v in sorted(pp["t_win_by_t_minus_ct_alive_at_plant"].items(), key=lambda kv: int(kv[0]))
        ],
        "by_time": [
            {"when": label, **rate(r["plant_timing"]["ALL"][key])}
            for label, key in [
                ("Before 0:45", "early (<0:45)"),
                ("0:45 to 1:15", "mid (0:45–1:15)"),
                ("After 1:15", "late (>1:15)"),
            ]
        ],
    }

    op = r["opening"]["ALL"]
    ev = r["events"]
    out["opening"] = {
        "opener_wins": rate(op["opener_wins"]),
        "ct": rate(op["CT_opens"]),
        "t": rate(op["T_opens"]),
        "traded": rate(ev["opening kill traded within 5 s"]["by_map"]["ALL"]),
        "spots": {
            k: {
                "openings": r["opening_places"][k]["openings"],
                "spots": [
                    {"place": place(s["place"]), "side": s["opener"], "share": s["share"], **rate(s)}
                    for s in r["opening_places"][k]["spots"][:SPOTS_PER_MAP]
                ],
            }
            for k in MAPS
        },
    }

    eco = r["economy"]["ALL"]
    cells = eco["matchups_ct_win"]
    out["economy"] = {
        "eco_beats_full": rate(eco["eco_beats_full"]),
        "force_beats_full": rate(eco["force_beats_full"]),
        "pistol_winner_wins_next": rate(eco["pistol_winner_wins_next"]),
        "buys": BUYS,
        "ct_win": {
            f"{ct} v {t}": rate(cells[f"CT {ct} v T {t}"])
            for ct in BUYS
            for t in BUYS
            if f"CT {ct} v T {t}" in cells and cells[f"CT {ct} v T {t}"]["n"] >= MIN_CELL
        },
    }

    cl = r["clutches"]["ALL"]
    out["clutches"] = [{"odds": k, "ct": rate(cl[f"CT {k}"]), "t": rate(cl[f"T {k}"])} for k in ["1v1", "1v2", "1v3+"]]

    pr = r["predictability"]
    out["predictability"] = {
        "base": pr["base"]["brier_all"],
        "map": pr["map"]["brier_all"],
        "econ": pr["econ"]["brier_all"],
        "maps": [
            {"map": k, "moments": v["moments"], "base": v["base"], "econ": v["econ"], "round_start": v["round_start_brier_econ"]}
            for k, v in sorted(pr["per_map"].items(), key=lambda kv: kv[1]["econ"])
        ],
    }

    wp = m["m1_win_probability"]
    usd = {
        "jev": m["jev_ledger"]["usd"] / m["jev_ledger"]["calls"],
        "claude": m["ledger"]["claude"]["usd"] / m["ledger"]["claude"]["calls"],
        "gpt-oss": None,
        "logistic": 0,
    }
    out["models"] = {
        "moments": wp["moments_compared"],
        "rows": [
            {
                "model": k,
                "brier": wp["by_model"][k]["raw"]["brier"],
                "auc": wp["by_model"][k]["raw"]["auc"],
                "usd_per_call": None if usd[k] is None else float(f"{usd[k]:.2g}"),
            }
            for k in sorted(wp["compared"], key=lambda k: wp["by_model"][k]["raw"]["brier"])
        ],
    }
    return out


def main():
    report, models, dest = sys.argv[1:4]
    with open(report, encoding="utf-8") as f:
        r = json.load(f)
    with open(models, encoding="utf-8") as f:
        m = json.load(f)
    with open(dest, "w", encoding="utf-8", newline="\n") as f:
        json.dump(build(r, m), f, indent=1)
        f.write("\n")


if __name__ == "__main__":
    main()
