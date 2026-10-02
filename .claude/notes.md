# Notes

Running log of decisions, progress and findings. Newest first. Keep entries short; move anything settled and permanent into CLAUDE.md or the spec.

## 2026-10-02

- Workspace initialized: `parser/` (Go module) and `web/` (Vite + Preact + TypeScript). Go 1.27 installed. `parser/demo` reads and validates the demo preamble; tests pass.
- Spec pasted into `.claude/spec.md`. It settles:
  - Cloud: Cloudflare (Worker API, R2, D1, Queues) in front, Cloud Run for the parser.
  - Frontend: **Vite + Preact**. The spec originally suggested React; we switched to Preact and updated the spec (prod bundle 220 KB -> 11 KB). Use `preact` and `preact/hooks`, not `react`.
  - Map for v1: Mirage.
  - Protobuf and snappy libraries: compatible with the spec. Only demo-parsing libraries are banned, and they may still be used as test oracles in `tools/`.
- Current milestone: **M0 (contract)**. Remaining:
  - [ ] Write schema v1 as JSON Schema in `schema/`, with sample files
  - [ ] Pick the reference parser and export fixtures from 3 demos (matchmaking, FACEIT or pro, one with overtime)
  - [ ] Create the Cloudflare and GCP accounts and the dev environment
  - [x] Create the repo
  - [x] Pick the map (Mirage)
- Repo vs spec gaps to close:
  - [ ] Add `worker/`, `schema/` and `tools/`
  - [ ] CLI: the binary is `demo` with `parse <file> --out <dir> [--events-only]` and `diff <file> --against <ref.json>`. Today it is `cmd/demoparse` and only checks the header.
  - [ ] On failure the CLI exits non-zero and prints one JSON error line on stderr.
  - [ ] Parser HTTP wrapper (`POST /parse`) and Dockerfile
- Still assumed: Go module path `github.com/ChiemekaKalu/demoview/parser`.
- The spec's other open questions are still open: reference parser choice, Queues on the paid plan, 16 vs 32 Hz, duplicate uploads, Steam ids in public data, public repo.
