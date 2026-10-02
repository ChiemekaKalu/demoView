# CI and deploys

| Workflow | Runs on | Does |
| -------- | ------- | ---- |
| `workflows/ci.yml` | pull requests, pushes to other branches, and every deploy | Go vet and tests (`parser/`, `tools/oracle/`), the parser diff against the oracle on the fixture demos, schema validation, Worker typecheck/lint/tests, web build/lint/tests, and a parser image build without a push. One job each, in parallel. |
| `workflows/deploy.yml` | pushes to `main`, or by hand | Runs every `ci.yml` check first and stops if any fails, since commits land on `main` without a pull request. Then deploys dev and, if dev succeeds, prod (via `deploy-env.yml`). |
| `workflows/deploy-env.yml` | called by `deploy.yml` | For one environment: the D1 migrations, then `npm run deploy:<env>` in `worker/`, which builds the viewer, builds the parser image from `parser/Dockerfile` with the runner's Docker, and deploys the Worker with its container. |

## Secrets

Set these at the **repository** level, so the checks that run inside a deploy can read them too.

| Name | What |
| ---- | ---- |
| `CLOUDFLARE_API_TOKEN` | An account API token with **Workers Scripts: Edit**, **Containers: Edit**, **D1: Edit**, **Queues: Edit** and **Account Settings: Read**. If a deploy reports a missing Cloudchamber permission, add **Cloudchamber: Edit** too (the older name for the container platform). |
| `CLOUDFLARE_ACCOUNT_ID` | The Cloudflare account id. The diff job also uses it to name the R2 endpoint. |
| `FIXTURES_R2_ACCESS_KEY_ID` | Optional: an R2 token with Object Read on `demoview-fixtures` only, for the diff job |
| `FIXTURES_R2_SECRET_ACCESS_KEY` | Optional: its secret |

Environments: create `dev` and `prod` under Settings → Environments. `prod` runs only after `dev` succeeds; add required reviewers to `prod` to approve each prod deploy by hand.

The Worker's own secrets (`UPLOAD_TOKEN`, `PARSE_SECRET`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`) live on Cloudflare, not GitHub; see `worker/README.md`.

## The parser diff test

The `diff` job in `ci.yml` downloads every `.dem` under `demos/` in the R2 bucket `demoview-fixtures`, exports each with `tools/oracle`, then runs `demo diff <file.dem> --against <export>/match.json`, which exits 1 with a report on any mismatch (the rules are in `parser/diff`). It needs the two `FIXTURES_R2_*` secrets and `CLOUDFLARE_ACCOUNT_ID`.

One-time: create the `demoview-fixtures` bucket and upload the fixture demos to `demos/<name>.dem`. Without these secrets the job skips with a notice, so forks and a new repo stay green.
