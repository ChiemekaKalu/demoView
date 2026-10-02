# Bulk pro-demo study (research branch only)

Tooling for the round-dynamics study. It lives on `research/bulk-demos`, which is never merged; only the write-up lands on `main`, under `.claude/ideas/rounds/`.

- **Runs in the cloud:** `.github/workflows/research-bulk.yml` runs on GitHub Actions when this branch is pushed. Nothing runs on a laptop.
- **Source:** public pro demos from the Hugging Face dataset `skkwowee/chimera-cs2`, pinned to one revision. `pick.py` picks a map-balanced sample (`config.json`: 16 per map, seed 2026, 12 shards).
- **Per demo,** one at a time per shard:
  1. download
  2. `bulkfacts` (name-free round facts)
  3. `oracle export`
  4. our `demo parse`
  5. `demo diff` against the oracle
  6. record exit codes, time and peak memory
  7. delete the demo
- **Storage:** results only, in `demoview-dev` under `research/bulk/<run id>/`:
  - `plan.json`
  - `shards/<n>.tar.gz` (facts and parser records)
  - `report.json`
  - `report.md`

  `r2put.sh` is the only writer, and it can't write anywhere else. Demos are never stored or rehosted. Diff reports, which can name players, stay in the run's Actions artifacts.
- **Privacy:** facts carry players only as a per-demo index, plus sides, weapons and callouts. The report is aggregates only.
- **Modes,** set in `config.json`, one per push:
  - `full`: the whole pipeline above, then the analysis and the Jev job.
  - `parser`: only our parser and the oracle diff. `demo` is built from `parser_ref` (a branch or commit), on `paths`, or on the default `parser_paths` (10 repro and control demos in 5 shards). The report gives name-free outcome categories, per-demo rounds (ours vs the reference) and mismatch kinds.
  - `analyze`: re-runs the analysis and the Jev job on a stored run (`source_run`), with no downloads.
- **Jev on pro play** (`jev_pro.py`, `"jev"` in `config.json`):
  - It asks Jev for the CTs' win chance at round start and after every kill, for a map-balanced sample of the stored facts.
  - It scores the answers raw and with the matchmaking temperature, against the logistic baseline.
  - It stops at the configured minutes or token cap. The token comes from the `CLOUDFLARE_JEV` secret and is never printed.
