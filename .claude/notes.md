# Notes

Running log of decisions, progress and findings. Newest first. Keep entries short; move anything settled and permanent into CLAUDE.md or the spec. Checkpoint status lives in `plan.md`, not here.

## 2026-10-02

- **Parser runtime: Cloudflare Containers, not Cloud Run.** Deliberate deviation from the spec. It's a hobby project and the $5/month Workers Paid plan buys one cloud, no public parser endpoint and no shared `PARSE_SECRET`. Cloud Run only if the project grows.
  - Containers: `standard-2` is 1 vCPU and 6 GiB, scale to zero after a 10-minute inactivity timeout, and the Worker calls it through a Durable Object.
  - Unconfirmed: the docs give no request-duration limit. A stub that holds a multi-minute request settles it. Update the spec once it passes, and fall back to Cloud Run if it fails.
- **Cloudflare dev and prod are set up.** D1 (schema applied), queues with DLQs, R2 buckets with CORS, and per-bucket R2 S3 tokens (scoped, tested) all exist, with secrets set on both Workers. Nothing is deployed. Names are `demoview-{dev,prod}` for D1 and R2, and `demoview-{dev,prod}-parse[-dlq]` for queues.
- **Narrative logging is a product output, not platform logs.** The parser should emit a structured, tick-stamped, versioned event log as a durable artifact in R2, which could become a ledger later. A design proposal (taxonomy, storage, parser API) is in progress, and the schema stays the user's call.
- **Map data in R2:** radars, `radar_info` and `available.json` from MurkyYT/cs2-map-icons at ae7ed6a, under `maps/` in both buckets with a manifest. Private, because the source repo has no license.
- **Later on 10-02:**
  - No Claude co-author trailer on commits from now on. Existing commits keep it.
  - v0 infers round winners and teams from events, because `round_end` and `round_start` are gone from demos since about Feb 2024.
  - Two public demos approved as interim fixtures.
  - M1 checkpoints 1.1 to 1.4 pass on both. `worker/`, `schema/`, `parser/pb` and `bitread` have landed.
  - Details are in `plan.md`.
- **Hard boundary, set by the user:** never touch their game memory, for any reason. Recorded in CLAUDE.md ground rules and sent to both peer sessions. Demo files are the only game input.
- Workspace initialized: `parser/` (Go module) and `web/` (Vite + Preact + TypeScript). Go 1.27 installed. `parser/demo` reads and validates the demo preamble; tests pass.
- Spec pasted into `.claude/spec.md`, reformatted as Markdown, then updated for a **solo build**:
  - One milestone sequence: M0 schema and fixtures, M1 v0 parser, M2 cloud pipeline on fixture data, M3 viewer, M4 entity decoding.
  - v1 ships on reference-parser round files by default. My entity decoding swaps in at M4 once it passes the position diff.
  - Sized for agent-driven work: about 3 to 4 weekends to v1, plus 2 to 3 for M4.
- Settled by the spec:
  - Cloud: Cloudflare (Worker API, R2, D1, Queues) in front, Cloud Run for the parser.
  - Frontend: **Vite + Preact**. The spec originally suggested React; we switched and updated the spec (prod bundle 220 KB -> 11 KB). Use `preact` and `preact/hooks`, not `react`.
  - Map for v1: Mirage.
  - Protobuf and snappy libraries are fine. Only demo-parsing libraries are banned, and those may still run in `tools/` as test oracles.
- Checkpoints for M0 and M1, plus spec gaps with proposed defaults, are in `.claude/plan.md`.
- Work split in parallel:
  - Subagents: frame loop and CLI (`parser/demo`, `parser/cmd`, `parser/match`), vendored protos (`parser/pb`, `go.mod`), bit reader (`parser/bitread`), schema and samples (`schema/`), and reference-parser research.
  - A peer Claude session: the `worker/` scaffold, committing with `git commit -- worker/`.
- Still assumed: Go module path `github.com/ChiemekaKalu/demoview/parser`.
