# Pickup prompt for the notes session

Read this first if you are taking over the wiki and its learning-center site. It says how everything is organised, how current it is, how to restart the site, how to do an upkeep pass, and what to do next. Written at a stopping point: v1 is built and the system is solidly functional.

## What you are looking after

Two things that share one source of truth, the markdown pages in this folder.

| Thing | Where | What it is |
| ----- | ----- | ---------- |
| The wiki | `.claude/wiki/*.md` | About 20 linked pages with mermaid diagrams. `README.md` is the front door. |
| The learning center | `.claude/wiki/site/` | A local Vite and TypeScript site that presents those pages as a guided walkthrough. It is not part of the product and nothing deploys it. |

The wiki explains; the spec (`../spec.md`), plan (`../plan.md`) and notes (`../notes.md`) are the sources of truth. **If a page disagrees with the code, the code wins.** Fix the page and add a changelog line.

### The pages, by group

| Group | Pages |
| ----- | ----- |
| Start | `README`, `big-picture` |
| How it works | `demo-format`, `parser`, `match-logic`, `schema`, `worker`, `viewer`, `pulse` |
| Running it | `testing`, `infra` |
| Operations | `maps`, `r2-layout`, `runbook-reparse`, `runbook-stuck-matches` |
| Reference | `decisions`, `glossary`, `changelog`, `pickup` |

The order and groups are `PATH` in `site/src/content.ts`. A page not listed there still shows up, under "More".

### The learning center's parts

| Tab | Source | Notes |
| --- | ------ | ----- |
| Tour | `site/src/tour.ts` | 11 steps following one demo through the system. Each step lights up parts of the diagram, may mount a lab, and links to pages. |
| Map | `site/src/views/map.ts` | Six components (browser, Worker, R2, D1, queue, parser), each with a role, locations, notes and links. |
| Lab | `site/src/widgets/` | Seven interactive models. Register one in `widgets/index.ts`. Use it in a page with `<!-- widget: name -->` or in a tour step with `widget: 'name'`. |
| Pages | the markdown files | Rendered with `marked` and `mermaid`. A contents panel, search and section filters. |
| Study | `site/src/quizzes.ts`, the glossary | Optional: read-progress, notes, quizzes, flashcards. Stored in the browser's `localStorage`. |

`site/README.md` explains how to add a tour step, a model, a quiz question or a flashcard. The styling is adapted from the owner's personal site, in a quieter register: no pixel scenes and no jokes.

## How current it is

- **Wiki and site are current to repo commit `cc4c6b3`** (the newest non-wiki commit at the last pass, "add opening kills, trades, multi-kills and clutches to the narrative log"), and the changelog's top entry says what that pass covered.
- **State of the project it describes:** v1 is built. M0 to M4 are done: the schema and fixtures, the v0 parser, the cloud pipeline (dev and prod live), the viewer, and entity decoding with **prod shipping our own round files**. M5 (maps, duplicate reuse, safe reparse, self-recovery) is closed, and M6's parser-side hardening and the dev shadow compare are done.
- **The last changelog entry** is the one at the top of `changelog.md`. The newest hash named in it is your "last seen" for the next pass.

## Restarting the site

It was stopped by the local off switch, and by the background time limit of the terminal that started it.

```sh
cd .claude/wiki/site
npm install        # only if node_modules is missing
npm run dev        # http://localhost:5180, opens the browser
```

- **Keep port 5180.** Read-progress, notes and quiz scores live in `localStorage`, which is per browser and per port.
- `npm run dev` stays in the foreground. In Claude Code, start it with `! cd .claude/wiki/site && npm run dev`, or run it in the background and expect it to be stopped at the time limit.
- **The local off switch:** `pwsh tools/local/stop-local.ps1` lists what it would stop, and `-Force` stops it. It stops this site too.
- `npm run build` typechecks and builds a static copy into `dist/`, which is gitignored. Delete it afterwards.
- `?theme=dark` or `?theme=light` previews a theme for one load.

## How to do an upkeep pass

This is what the recurring job did every 25 minutes. The job lives in a session and dies with it, so recreate it if you want it back.

1. Find the last-seen hash: the newest hash named in `changelog.md`.
2. `git log <hash>..HEAD`, `git status --short`, and diff `.claude/plan.md`, `notes.md`, `logging.md`, `spec.md` and `CLAUDE.md` since then.
3. Read any new or changed source that matters. Commit messages and the plan's checkpoint notes carry the numbers.
4. Update only what moved: the pages, and where a fact changed, the site (`tour.ts` steps, `views/map.ts` details, `quizzes.ts` answers). Keep quiz answers and tour text true to the pages.
5. Add a dated changelog entry naming the commits. Correct anything that turned out wrong.
6. Run `npx tsc --noEmit` in `site/` after touching it.
7. Commit: `git add` only the changed files under `.claude/wiki`, never `-A` or `-a`, then `git commit -m "docs(wiki): ..." -- .claude/wiki` (use `feat(wiki)` or `fix(wiki)` for site code). Small logical commits, **no Claude co-author trailer**, and **never push**.
8. If nothing changed, do nothing and say so in one line.

### Rules that matter

- **Never name the user's demos, players or Steam ids.** Call them "the user's own matches" or "8 MM/Premier demos". The public pro demos are public, but don't name the dataset.
- **Say plainly what is built and what is only planned.** Several pages carry a status line for exactly this.
- **Don't edit code outside `.claude/wiki`,** and don't edit the spec, plan or notes. Send changes to their owner.
- Sessions, by what they own: `parser` (`parser/`, `schema/`), `pipeline` (`worker/`, `web/`), `cloud` (infra and coordination), and the notes session (this wiki). `cloud` pings with a commit hash and the page it affects.

### Gotchas learned the hard way

- **A pattern replace hits the first match.** One edit once matched the first mermaid block in the README and deleted a section. Check what a pattern matched before replacing, and prefer an exact string or an anchored regex.
- **The files have Windows line endings.** A regex using `.` stops before `\r`, which made every article show its title twice. Strip with `\r?\n`, and read and write files with `newline=''`.
- **Apostrophes in TypeScript strings.** A tour or map text in single quotes needs `\'`. If `tsc` fails after an edit, look at the line it names.
- **Keep wiki links as `page.md#heading`.** The site rewrites them to routes. Heading ids follow GitHub's rules, so a renamed heading breaks its anchors, and anchors appear in several pages.
- **CRLF warnings on commit are harmless.**

## What to update next

1. **Mark v1 done.** The only v1 item left is the user's own desktop scrub check of the viewer. When it passes, update the README's viewer line and the milestone diagram, and remove "on real parses next" from the diagram label.
2. **The narrative log.** The parser writes it with `demo parse`, but the server doesn't ship it, and its R2 key is undecided (`../logging.md` has the open question). When that is decided, update the decisions page, and write a page for the log's output.
3. **v1.2** is decided: the Valve fetch (not started), two-floor maps with a schema version bump, the narrative log's server side and story pane, and Match Pulse's shelf (the line and kill marks are already in the viewer).
4. **Open follow-ups:** ADR gaps of 2 to 11% on five full pro recordings, with the cause unknown, and the optional CI fixture-diff requirement, which needs read-only secrets and a repo variable.
5. **Learning-center gaps worth filling:** quiz questions for the maps, R2 layout, runbook and Pulse pages, a tour step on the reparse and pointer swap, and a lab for the versioned swap (new prefix, manifest last, pointer swap).
6. **Pages most likely to drift:** `worker.md` (routes and background jobs), `infra.md` (CI and deploy), `match-logic.md` (the tournament-demo section), and the README's status block. Re-read them against the code every few passes.
