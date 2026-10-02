"""Find where our ADR damage and the game's damage counter part ways, round by round.

For each round of our match.json, the game's damage_total counter is read at the
next round's start (or the win panel), so post-round damage is included, as it is
in ours. Our damage is recomputed from player_hurt the way matchlogic does: the
victim's health before the hit minus the health after it, enemy hits only, health
reset at spawn. Rounds where the two differ are printed with the hits behind the
difference. Players are named by match.json idx only: no names or Steam ids.

demoparser2 reads the game's counters here as a black box, the way tools/oracle uses
demoinfocs: a reference only, never imported by parser/. It runs in the research
workflow, which downloads public demos in CI, never on the user's machine.

Usage: adr_rounds.py <demo.exe> <demo.dem>...   (needs `pip install demoparser2`)
"""
import json, os, subprocess, sys, tempfile
from collections import defaultdict
from demoparser2 import DemoParser

exe, demos = sys.argv[1], sys.argv[2:]
for path in demos:
    out = tempfile.mkdtemp()
    subprocess.run([exe, "parse", path, "--out", out, "--events-only"], check=True, capture_output=True)
    ours = json.load(open(os.path.join(out, "match.json"), encoding="utf-8"))
    idx = {p["steam_id"]: p["idx"] for p in ours["players"] if p["steam_id"] != "0"}
    rounds = ours["rounds"]

    p = DemoParser(path)
    win = p.parse_event("cs_win_panel_match")
    end_tick = int(win["tick"].max()) if len(win) else None
    cut = [r["start_tick"] for r in rounds[1:]] + [end_tick if end_tick is not None else rounds[-1]["end_tick"] + 1]
    game = p.parse_ticks(["damage_total"], ticks=sorted(set(cut + [rounds[0]["start_tick"]])))
    counter = defaultdict(dict)  # tick -> idx -> damage_total
    for _, g in game.iterrows():
        i = idx.get(str(g["steamid"]))
        if i is not None:
            counter[int(g["tick"])][i] = int(g["damage_total"])

    hurts = p.parse_event("player_hurt", player=["team_num"])
    spawns = p.parse_event("player_spawn")
    evs = [(int(h["tick"]), 1, h) for _, h in hurts.iterrows()] + [(int(s["tick"]), 0, s) for _, s in spawns.iterrows()]
    evs.sort(key=lambda e: (e[0], e[1]))

    print(f"== {os.path.basename(path)} {ours['match']['map']} {len(rounds)} rounds")
    health, mine, hits = {}, defaultdict(int), defaultdict(list)
    k, shown = 0, 0
    prev = {i: counter[rounds[0]["start_tick"]].get(i, 0) for i in idx.values()}
    for r, t in enumerate(cut):
        while k < len(evs) and evs[k][0] < t:
            tick, kind, e = evs[k]
            k += 1
            if tick < rounds[0]["start_tick"]:
                continue
            if kind == 0:
                health.pop(str(e.get("user_steamid")), None)
                continue
            v, a = str(e.get("user_steamid")), str(e.get("attacker_steamid"))
            before = health.get(v, 100)
            after = int(e["health"])
            health[v] = after
            if a in idx and v != a and e.get("attacker_team_num") != e.get("user_team_num"):
                d = max(0, before - after)
                mine[idx[a]] += d
                hits[idx[a]].append(f"t{tick} -> {idx.get(v, '?')} before {before} after {after} dmg_health {e.get('dmg_health')} = {d}")
        now = counter.get(t, {})
        for i in sorted(idx.values()):
            gd, od = now.get(i, 0) - prev.get(i, 0), mine[i]
            if gd != od and shown < 12:
                shown += 1
                print(f"  round {r + 1}, idx {i}: game +{gd}, ours +{od}")
                for line in hits[i][:8]:
                    print(f"      {line}")
        prev = {i: now.get(i, prev.get(i, 0)) for i in idx.values()}
        mine, hits = defaultdict(int), defaultdict(list)
    if shown == 0:
        print("  every round agrees")
