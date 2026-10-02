# CI and deploys

| Workflow | Runs on | Does |
| -------- | ------- | ---- |
| `workflows/ci.yml` | pull requests, pushes to other branches, and every deploy | Go vet and tests (`parser/`, `tools/oracle/`), the parser diff against the oracle on the fixture demos, schema validation, Worker typecheck/lint/tests, web build/lint/tests, and a parser image build without a push. One job each, in parallel. |
| `workflows/deploy.yml` | pushes to `main`, or by hand | Runs every `ci.yml` check first and stops if any fails, since commits land on `main` without a pull request. Then builds and pushes the parser image once, deploys dev and, if dev succeeds, prod (via `deploy-env.yml`). |
| `workflows/deploy-env.yml` | called by `deploy.yml` | For one environment, in order: Cloud Run, the D1 migrations, then the Worker and viewer. |

The Worker gets `PARSER_URL` from the Cloud Run service that the same run just deployed (`wrangler deploy --var`), so the placeholder in `worker/wrangler.jsonc` never ships. A deploy from a laptop (`npm run deploy:dev`) still uses the placeholder, so pass `-- --var PARSER_URL:<url>` there too.

## Secrets and variables

Set these at the **repository** level. The image job runs outside any environment, so environment-level secrets would not reach it.

| Name | Kind | What |
| ---- | ---- | ---- |
| `GCP_PROJECT_ID` | variable | The GCP project |
| `GCP_WORKLOAD_IDENTITY_PROVIDER` | secret | `projects/<number>/locations/global/workloadIdentityPools/<pool>/providers/<provider>` |
| `GCP_SERVICE_ACCOUNT` | secret | The deployer service account's email |
| `CLOUDFLARE_API_TOKEN` | secret | Workers Scripts: Edit, D1: Edit and Queues: Edit on the account (the "Edit Cloudflare Workers" template plus D1 and Queues) |
| `CLOUDFLARE_ACCOUNT_ID` | secret | The Cloudflare account id. It is also passed to Cloud Run as `R2_ACCOUNT_ID`. |

Environments: create `dev` and `prod` under Settings → Environments. `prod` runs only after `dev` succeeds; add required reviewers to `prod` to approve each prod deploy by hand.

## One-time setup

GCP, in region `us-west1`:

1. Enable the Cloud Run, Artifact Registry, Secret Manager and IAM Credentials APIs.
2. Create a Docker repository named `demoview` in Artifact Registry. Images land at `us-west1-docker.pkg.dev/<project>/demoview/parser:<sha>`.
3. Create a Workload Identity pool and an OIDC provider for `https://token.actions.githubusercontent.com`, restricted to this repository. Let it impersonate the deployer service account (`roles/iam.workloadIdentityUser`).
4. Give the deployer service account `roles/run.admin` (it also sets the public invoker policy), `roles/artifactregistry.writer`, and `roles/iam.serviceAccountUser` on the Cloud Run runtime service account (the default compute account unless you choose another).
5. Create these Secret Manager secrets, each for `dev` and `prod`, and give the runtime service account `roles/secretmanager.secretAccessor` on them:
   - `demoview-<env>-parse-secret`: must equal the Worker's `PARSE_SECRET` in that environment
   - `demoview-<env>-r2-access-key-id` and `demoview-<env>-r2-secret-access-key`: the R2 token for the `demoview-<env>` bucket

Cloudflare is already set up (see `worker/README.md`): the D1 databases, buckets, queues, Worker secrets and bucket CORS rules exist.

## The parser diff test

The `diff` job in `ci.yml` downloads every `.dem` under `demos/` in the R2 bucket `demoview-fixtures`, exports each with `tools/oracle`, then runs `demo diff <file.dem> --against <export>/match.json`, which exits 1 with a report on any mismatch (the rules are in `parser/diff`). It needs:

| Name | Kind | What |
| ---- | ---- | ---- |
| `FIXTURES_R2_ACCESS_KEY_ID` | secret | An R2 token with Object Read on `demoview-fixtures` only |
| `FIXTURES_R2_SECRET_ACCESS_KEY` | secret | Its secret |
| `CLOUDFLARE_ACCOUNT_ID` | secret | Already set for deploys; it also names the R2 endpoint |

One-time: create the `demoview-fixtures` bucket and upload the fixture demos to `demos/<name>.dem`. Without these secrets the job skips with a notice, so forks and a new repo stay green.
