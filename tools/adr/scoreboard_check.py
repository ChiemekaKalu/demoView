"""Compare our match.json scoreboard with the game's own scoreboard counters.

The in-game scoreboard reads the controller's match stats; demoparser2 is used
only as a black box to read them at the cs_win_panel_match tick.
A local guard for the user's own matchmaking demos in parser/testdata/demos:
every parser change should still print ALL MATCH. demoparser2 is a reference
only, as in adr_rounds.py. Needs `pip install demoparser2`.

Usage: scoreboard_check.py <demo.exe> <demo.dem>...
"""
import json, subprocess, sys, tempfile, os
from demoparser2 import DemoParser

exe, demos = sys.argv[1], sys.argv[2:]
props = ["kills_total", "deaths_total", "assists_total", "headshot_kills_total", "damage_total"]
all_ok = True
for path in demos:
    out = tempfile.mkdtemp()
    subprocess.run([exe, "parse", path, "--out", out], check=True)
    ours = json.load(open(os.path.join(out, "match.json"), encoding="utf-8"))
    rounds = len(ours["rounds"])
    sb = {r["player"]: r for r in ours["scoreboard"]}
    mine = {p["steam_id"]: sb[p["idx"]] for p in ours["players"] if p["steam_id"] != "0"}

    p = DemoParser(path)
    win = p.parse_event("cs_win_panel_match")
    tick = int(win["tick"].max())
    game = p.parse_ticks(props, ticks=[tick])

    bad = []
    for _, g in game.iterrows():
        sid = str(g["steamid"])
        if sid not in mine:
            continue
        o = mine[sid]
        hs = round(o["hs_pct"] * o["kills"] / 100)
        gadr = round(int(g["damage_total"]) / rounds, 1)
        want = (int(g["kills_total"]), int(g["assists_total"]), int(g["deaths_total"]), int(g["headshot_kills_total"]), gadr)
        got = (o["kills"], o["assists"], o["deaths"], hs, o["adr"])
        if want != got:
            bad.append(f"{g['name']}: game K/A/D/HS/ADR {want} vs ours {got}")
    print(f"{os.path.basename(path)} {ours['match']['map']} {rounds} rounds: "
          f"{len(mine) - len(bad)}/{len(mine)} players match exactly")
    for b in bad:
        print("   ", b)
    all_ok &= not bad
print("ALL MATCH" if all_ok else "MISMATCHES ABOVE")
