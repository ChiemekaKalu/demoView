# Glossary

**ADR.** Average damage per round: enemy damage over all live rounds. Each hit counts as the victim's HP before minus HP after, so overkill doesn't inflate it.

**Baseline.** The starting state of an entity class, stored in a string table. Entities are created from it, then updated. (M4.)

**`begin_new_match`.** The game event that starts the live match. Match logic's window opens at the *last* one.

**Bitstream.** The packed format inside `DEM_Packet`. Messages are not byte-aligned, which is why `bitread` exists.

**Consistency guard.** The server check that ships our `match.json` only when it agrees with the oracle's. See [parser](parser.md#the-server-and-the-consistency-guard).

**`cs_win_panel_match`.** Fires when the match ends. The end of the live window.

**Demo / `.dem`.** A recorded match. Only server-recorded CS2 demos are supported, with no POV demos or CS:GO.

**DLQ.** Dead letter queue. After 3 failed attempts a parse job lands here and the match is marked `failed`.

**Entity.** A game object (player pawn, grenade, game rules) whose properties the server streams as updates. Decoding them is M4.

**Fallback rule.** The spec's way of de-risking M4: v1 ships on oracle round files, and ours swaps in when it passes.

**Field path.** A Huffman-coded address of a changed property within an entity update.

**Frame.** Two things, so check which. In a *demo*, a command with a tick and a payload. In a *round file*, one sample of every player (every tick, 64 Hz, in the spec).

**Freeze time.** The start-of-round countdown where nobody can move. Round files start where it ends.

**FullPacket.** A snapshot frame. Its string tables are deltas, not complete.

**Half / halftime.** Sides swap after round 12 (MR12) and at each overtime block's midpoint. `announce_phase_end` marks it.

**Live window.** The part of the demo that counts: after the last `begin_new_match`, up to `cs_win_panel_match`.

**Match logic.** `parser/matchlogic`. Events in, rounds, sides, kills and scoreboard out.

**MR12.** Max rounds 24: first to 13 wins. Overtime defaults to MR3 blocks (6 rounds).

**Narrative log.** The proposed tick-stamped record of facts, derived events and parser notes. See [decisions](decisions.md#narrative-log).

**Note.** A plain-English record that match logic had to guess. Never changes output.

**Oracle.** `tools/oracle`, our reference exporter on demoinfocs. Answer key for tests, and source of v1's round files.

**Participant.** A stable identity in match logic. A *slot* can change hands, but a participant can't.

**Preamble.** `PBDEMS2\0` plus two int32 offsets at the start of a demo.

**Round file.** `rounds/NN.json`. Per-player columns, one frame per tick (64 Hz in the spec), plus grenades and kill events.

**`side_a`.** Which side (CT or T) team A played that round.

**Slot.** A player's seat number, `userid & 0xFF`. Keys the `userinfo` string table.

**String table.** Key-value tables the server syncs: `userinfo` (identities), baselines and so on.

**Team A / B.** Fixed for the whole match. A is the team on CT in round 1. `side_a` tracks the swaps.

**Tick.** The server's time step (64 per second). We use the demo-frame tick, never `net_Tick`, which is on an offset clock.

**userid.** An event's player reference. Low byte is the slot, and 65535 or -1 means no player.

**xuid.** SteamID64 in `CMsgPlayerInfo`. 0 for bots.
