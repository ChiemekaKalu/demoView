# demoView

A web app where you hand over a CS2 demo (`.dem`) and get back a scoreboard and a scrubbable 2D replay of every round. The parser is written from scratch in Go. Everything else is cloud plumbing around it: Cloudflare (Worker API, R2, D1, Queues) in front and Cloud Run for parsing.

## Spec and notes

- `.claude/spec.md` is the full product spec: scope, architecture, schema, API, milestones. If it disagrees with this file, the spec wins; update this file to match. The exception is a deliberate deviation recorded in the notes.
- `.claude/plan.md` breaks the milestones into checkpoints, tracks their status, and lists spec gaps with proposed defaults. Check items off as they land.
- `.claude/notes.md` is the running log of decisions, progress and findings. Add a dated entry when a decision is made or something non-obvious is learned.

@.claude/spec.md
@.claude/plan.md
@.claude/notes.md

## Layout

| Path      | What                                                                   |
| --------- | ---------------------------------------------------------------------- |
| `parser/` | Go module `github.com/ChiemekaKalu/demoview/parser`: the demo parser and its binaries |
| `worker/` | Cloudflare Worker: API routes, parse queue consumer, D1 migrations. API response shapes and M2 setup steps are in `worker/README.md` |
| `web/`    | Viewer: static Vite + Preact + TypeScript single-page app              |

Not created yet (see the spec's repo layout): `schema/`, `tools/`.

## Commands

```sh
# parser
cd parser && go test ./...
cd parser && go run ./cmd/demo parse path/to/match.dem --out out/
cd parser && go run ./cmd/demo inspect path/to/match.dem   # dev: frame stats

# worker
cd worker && npm test           # vitest in the Workers runtime
cd worker && npm run typecheck
cd worker && npm run lint       # oxlint
cd worker && npm run dev        # wrangler dev, with D1 and Queues emulated locally

# web
cd web && npm run dev      # local dev server
cd web && npm run build    # typecheck + production build
cd web && npm run lint     # oxlint
```

## Ground rules

- **HARD BOUNDARY: never touch the user's game memory. Not for any reason.** No reading, writing, attaching to, injecting into or scanning the memory of CS2 or any other game process. No debuggers, memory readers, overlays, hooks or process handles aimed at a game. The only game input is a `.dem` file the user hands over. If a task seems to need live game access, stop and ask the user.
- **The parser is from scratch.** Reference parsers (demoinfocs-golang, demoparser, etc.) are test oracles only. They may run in `tools/` to export reference output, but `parser/` never imports them or copies from them. Generic building blocks such as a protobuf runtime and snappy are fine.
- **The schema is the contract.** Don't change schema files on your own; propose the change to the user first. Any breaking change bumps `schema_version`.
- **The parser streams.** Never load the whole demo into memory. The targets are under 512 MB and under 60 s for a full match on 1 vCPU.
- **Never commit demos.** Fixture demos live in a storage bucket. Local copies go in `parser/testdata/demos/`, which is gitignored.

## Commits

- Conventional Commits in small logical parts. Scope by top-level directory, e.g. `feat(parser): ...` or `docs(worker): ...`.
- **No `Co-Authored-By: Claude ...` trailer** (the user's instruction, until they say otherwise).
- Several sessions share this working tree. Commit only your own paths: `git commit -m "..." -- <paths>`. Never use `git add -A` or `git commit -a`.

## CS2 demo format notes

- **Preamble:** `PBDEMS2\0` followed by two little-endian int32 offsets (file info, spawn groups). See `parser/demo/header.go`.
- **Command stream:** varint command type, varint tick, varint size, then a protobuf payload from `demo.proto`. If bit `0x40` of the command type is set, the payload is snappy-compressed.
- **Net messages:** `DEM_Packet` and `DEM_SignonPacket` wrap a bitstream of net messages. Each one is a ubitvar type, a varint size, then the protobuf body.
- **Entities:** serializers come from `DEM_SendTables` (`CSVCMsg_FlattenedSerializer`). Updates arrive in `svc_PacketEntities` with Huffman-coded field paths.
- **Game events:** descriptors are in `CMsgSource1LegacyGameEventList` (msg 205), and events arrive as `CMsgSource1LegacyGameEvent` (msg 207). Event ids shift between game patches, so always map them by name from the descriptor list. Since about Feb 2024, `round_start`, `round_end`, `round_mvp` and `cs_win_panel_round` are never fired, even though their descriptors remain.
- **Player identity:** an event's `userid & 0xFF` is the player slot. The `userinfo` string table is keyed by that slot, and its values are `CMsgPlayerInfo`, whose `xuid` is the SteamID64 (0 for bots). It arrives as plain protobuf in `DEM_StringTables` and in each `DEM_FullPacket`.
- **Ticks:** use the demo-frame tick. `net_Tick` runs on a different, offset clock.
- **Protobuf schemas:** SteamDatabase/GameTracking-CS2 on GitHub (`Protobufs/`). Pin the version used.
