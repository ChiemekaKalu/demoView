# Notes

Running log of decisions, progress and findings. Newest first. Keep entries short; move anything settled and permanent into CLAUDE.md or the spec. Checkpoint status lives in `plan.md`, not here.

## 2026-10-02

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
