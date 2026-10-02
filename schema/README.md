# schema

The data contract between the parser and everything that reads its output (schema_version 1), plus Match Pulse's `pulse.json` (pulse_version 1, versioned on its own). The spec's Data model section is the source for the first two files and `.claude/ideas/jev/spec.md` for the third; these files encode them.

| File | What |
| ---- | ---- |
| `match.schema.json` | JSON Schema (draft 2020-12) for `match.json` |
| `round.schema.json` | JSON Schema for `rounds/NN.json` |
| `pulse.schema.json` | JSON Schema for `pulse.json`, which the Worker's pulse consumer writes beside a parse's `match.json` |
| `validate.mjs` | Validator: the schema plus cross-field checks (column lengths, frame ranges, unique ids, references into `match.json`) |
| `validate.test.mjs` | Checks that the validator rejects pulse files that break its cross-field rules |
| `samples/` | Synthetic sample data for building the viewer without a backend, and `generate.mjs`, which regenerates it deterministically |

```sh
cd schema && npm install
npm test                                              # validate every sample (--strict), then run validate.test.mjs
npm run validate -- out/match.json out/rounds/01.json # validate parser output
npm run validate -- out                               # or a whole output directory
npm run generate                                      # rewrite samples/ (same seed, same bytes)
```

A round file is checked against `../match.json` and a pulse file against the `match.json` beside it, when one exists, or else against the only `match.json` passed with it. Errors are rule breaks and fail the run. Warnings flag the conventions below, which the spec doesn't settle yet. `--strict` makes warnings fail too.

## Recommended conventions (pending decision)

The samples follow these, and the validator warns when output doesn't:

- `idx` is 0..N-1 and equals the array position; team A is the team on CT in round 1; rounds are numbered 1..N in order.
- `kills` are sorted by tick, and each tick falls between its round's `start_tick` and the next round's `start_tick`.
- `scoreboard` has one row per player and agrees with `kills`. `hs_pct` is on a 0–100 scale.
- Round files are named `NN.json`, zero-padded to two digits.
- A round file's `start_tick` is the round's `freeze_end_tick`. Frame `i` is tick `start_tick + i * tick_rate / hz`.
- A kill event's `frame` is `ceil((tick - start_tick) * hz / tick_rate)`, the first frame where the victim's `hp` is 0. `kill` is a 0-based index into `match.json` `kills`.
- A dead player keeps `hp` 0 and holds the position they died at.
- A grenade `path` has one point per frame from `throw_frame` to `detonate_frame`, inclusive.
- `pulse.json` `rounds` are sorted by `n`, with one entry per `match.json` round that doesn't end at its freeze end. A round's first moment is at its `freeze_end_tick`, then there is one moment per distinct tick of an in-round kill (after `freeze_end_tick`, up to `end_tick`), so kills on the same tick share a moment.

## Samples

`samples/` is synthetic: fake players, Steam ids `76561199999999001`–`010`, and `parser_version` `0.0.0-synthetic`. It is a 22-round de_mirage match, 13–9, MR12. Round files exist for rounds 1, 2, 16 and 21. Movement follows a hand-made waypoint graph whose coordinates only approximate Mirage callouts. Yaw is in integer degrees in [0, 360), counterclockwise from +x. Grenade types are `smoke`, `flash`, `he`, `molotov`, `incendiary` and `decoy`. `pulse.json` covers all 22 rounds, with model `jev-0.0.0-synthetic`: its odds come from a toy logistic model of alive counts and the bomb, not from Jev.
