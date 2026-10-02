# Bulk pro-demo study (research branch only)

Tooling for the round-dynamics study. It lives on `research/bulk-demos`, which is never merged; only the write-up lands on `main`, under `.claude/ideas/rounds/`.

- **Runs in the cloud:** `.github/workflows/research-bulk.yml` runs on GitHub Actions when this branch is pushed. Nothing runs on a laptop.
- **Source:** public pro demos from the Hugging Face dataset `skkwowee/chimera-cs2`, pinned to one revision. `select.py` picks a map-balanced sample (`config.json`: 16 per map, seed 2026, 12 shards).
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
- **Re-analysis without re-downloading:** set `"mode": "analyze"` and `"source_run": "<run id>"` in `config.json` and push. Only the aggregate job runs.
