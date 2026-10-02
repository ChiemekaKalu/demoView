# demoView

Hand over a CS2 demo and get back a scoreboard and a scrubbable 2D replay of every round.

- `parser/`: CS2 demo parser, written from scratch in Go
- `web/`: viewer (Vite + Preact + TypeScript)

## Getting started

Requires Go 1.25+ and Node 22+.

```sh
cd parser && go test ./...
cd parser && go run ./cmd/demo parse path/to/match.dem --out out/
cd parser && go run ./cmd/demo inspect path/to/match.dem   # dev: frame stats

cd web && npm install && npm run dev
```
