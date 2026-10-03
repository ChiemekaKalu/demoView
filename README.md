# demoView

Hand over a CS2 demo and get back a scoreboard and a scrubbable 2D replay of every round.

The parser is written from scratch in Go. Everything else runs on Cloudflare: one Worker serves the API and the viewer, R2 holds demos and parsed files, D1 holds match summaries, and a queue feeds a Container that runs the parser. Upload a demo, it parses in the cloud, and the match gets an unlisted share link.

## Layout

| Path | What |
| ---- | ---- |
| `parser/` | The demo parser: the `demo` CLI and the parse service that runs in the container |
| `worker/` | The Worker: API routes, queue consumers, crons and D1 migrations. Routes and response shapes are in [`worker/README.md`](worker/README.md) |
| `web/` | The viewer (Vite + Preact + TypeScript) |
| `schema/` | The contract: JSON Schemas for `match.json`, round files and `pulse.json`, a validator, and samples to build against |
| `tools/` | `oracle/` is the reference exporter, a test oracle the parser never imports. `align/` checks radar alignment, `adr/` holds research scripts, and `local/` stops local dev servers |
| `.github/` | CI and deploys. Secrets and setup are in [`.github/README.md`](.github/README.md) |

## Pages

| Route | Shows |
| ----- | ----- |
| `/` | Upload form and featured matches |
| `/m/:id` | Parse status, then the scoreboard, round list and pipeline history |
| `/m/:id/r/:n` | 2D replay of one round, with Match Pulse's win-chance strip where it's enabled |
| `/findings` | What decides a CS2 round, from pro demos |

Matches are unlisted: the gallery shows only matches the upload-token holder features.

## Getting started

Requires Go 1.25+ and Node 22+.

```sh
cd parser && go test ./...
cd parser && go run ./cmd/demo parse path/to/match.dem --out out/

cd worker && npm install && npm test
cd worker && npm run dev                 # wrangler dev, D1 and Queues emulated locally

cd web && npm install && npm run dev
cd web && VITE_FIXTURES=1 npm run dev    # the whole app on sample data, no backend

cd schema && npm install && npm test
```

Demos are never committed. Local copies go in `parser/testdata/demos/`, which is gitignored.

A merge to `main` runs CI, then deploys dev, then prod.

## Docs

- `.claude/spec.md` is the product spec, and `.claude/plan.md` tracks its checkpoints.
- `.claude/wiki/README.md` is a tour of how it all fits together.
