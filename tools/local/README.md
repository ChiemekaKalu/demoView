# Local off switch

`stop-local.ps1` stops the dev servers and helpers that the demoView sessions start on this machine. It lists by default and stops only with `-Force`.

```powershell
pwsh tools/local/stop-local.ps1          # list what would stop
pwsh tools/local/stop-local.ps1 -Force   # stop it
```

It matches processes whose command line runs one of `vite`, `wrangler`, `workerd`, `esbuild`, `astro`, `vitest` or a `poller.mjs`, from this repo or from `chiemeka.dev`. It never touches Claude Code, VS Code, shells, Steam or the game.

## What runs locally

Inventory taken 2026-10-02, about 04:55 Pacific.

| What | Started by | Port | Notes |
| ---- | ---------- | ---- | ----- |
| Wiki learning-center site (`vite --open` in `.claude/wiki/site`) | notes | 5180 | |
| `wrangler tail demoview-container-stub` | pipeline | none | Stale. The stub Worker was deleted after the Containers test. |
| esbuild service under `worker/node_modules` | pipeline | none | Child of the stale tail. |
| `poller.mjs` (reparse exit check on dev) | pipeline | none | Exits by itself after 75 minutes. |
| `astro build` in `chiemeka.dev` | the chiemeka.dev sessions | none | Short-lived. |

Nothing else listens on a port for demoView. The viewer's `npm run dev` (port 5173) and `wrangler dev` (port 8787) weren't running at inventory time. They are stopped by the same script when they are.

All of production runs on Cloudflare, so stopping everything here affects only local tooling.
