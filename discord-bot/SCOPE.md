# demoView Discord bot: scope

Oct 7, 2026 · draft for @chiemeka · nothing built yet

## Summary

A Discord bot that brings demoView matches into a server. It posts scoreboards and round links in text. In a voice channel it casts a round out loud, and later it answers spoken questions about the match.

- **v0 is text.** Slash commands post a match's scoreboard and round links. No voice, no AI.
- **v1 is a caster.** `/cast` joins your voice channel and narrates one round, read from the parsed files. The bot speaks but never listens.
- **v2 is a co-caster you can talk to.** Push-to-talk questions in voice ("who won the AWP duel on B?"), answered from match data through tool calls.

It reads the demoView API like the viewer does. It never parses demos, never writes to D1 or R2, and never touches the game client.

## Voice model choice

Google split its speech models in the Sept 2026 release. Translation is its own model, so the one you want is plain `gemini-3.8-live` and not the translate model.

| Model | What it is | Use here |
| --- | --- | --- |
| `gemini-3.8-flash-lite-tts` | Text to speech, GA. 100+ languages, prebuilt and designed voices, style notes and tags like `<laugh>` and `<short pause>`, multi-speaker. $0.50/M text in, $0.0015 per 10 s of audio out. | **v1 caster.** Code writes the script and this model reads it. Cheapest, and the facts stay in code. |
| `gemini-3.8-flash-tts` | The bigger TTS model, "studio-grade". $0.00225 per 10 s. | Swap in if Flash-Lite's delivery sounds flat. Same API. |
| `gemini-3.8-live` | The default Live API model: low-latency audio in and out, tool calling (non-blocking by default), no reasoning delay. $0.005/min in, $0.018/min out. | **v2 co-caster.** |
| `gemini-3.8-live-extended-thinking` | Live with deeper reasoning. | Not needed. The answers come from tools, not reasoning. |
| `gemini-3.5-live-translate-preview` | Speech-to-speech translation, preview. | **Skip.** This is the "translation stuff". |
| `gemini-3.5-transcribe-live` | Live speech to text. | Not needed, since `gemini-3.8-live` hears audio directly. |

**Recommendation:** Flash-Lite TTS for v1 and 3.8 Live for v2. Both are GA and on the free tier for development.

**Rough cost:** a two-minute round cast is about $0.02 of TTS. A five-minute v2 voice session is about $0.12 with the mic open the whole time, and less with push-to-talk.

## Commands

| Command | Version | What it does |
| --- | --- | --- |
| `/match <link or id>` | v0 | An embed with the map, the score, the top 3 by ADR, and a link to the match page. |
| `/round <match> <n>` | v0 | The round's winner, reason and kill list, with a viewer link. `t:42` adds `?t=42`. |
| `/recent` | v0 | The gallery's featured matches (`GET /api/gallery`). |
| `/cast <match> <n>` | v1 | Joins your voice channel, reads the round's cast, then leaves. One cast per guild at a time. |
| `/cast-match <match>` | v1 | A 60–90 s recap of the whole match: score by half, the decisive rounds, and the top fragger. |
| `/stop` | v1 | Stops speaking and leaves. |
| `/ask <match>` | v2 | Joins voice in Q&A mode. It listens only to the person who ran it, with push-to-talk, and only until it times out (5 min idle, 15 min max). |

Uploading a demo through the bot (an attachment, then `POST /api/matches`) is **out of scope**. Discord caps attachments well under a demo's size, and the upload token stays with you. That could change once the v1.2 Valve fetch lands.

## Where the words come from

The demoView spec already decided that facts are computed in code and models only phrase them (Jev spec, point 5: counting questions fail at 31%). The bot follows the same rule.

- **v1 cast script:** code builds it from `match.json`, the round file and, where they exist, `pulse.json` and the narrative log. It covers the opening kill, trades, the bomb plant and its site, the clutch, the biggest win-chance swing and the result. The script is plain text with style notes. TTS reads it word for word, so nothing gets made up.
  - Optional later: an LLM pass that makes the script sound less templated, with a check against the facts, as in the Jev spec's round-recap idea.
- **v2 answers:** `gemini-3.8-live` gets a short system prompt and these tools, all backed by the Worker API:
  - `get_scoreboard(match)`
  - `get_round(match, n)`: winner, reason, kills, bomb
  - `get_player(match, name)`: fuzzy name match, then the player's K/D/A, ADR and best rounds
  - `get_kill_at(match, round, time)`
  - `get_pulse(match, n)`: win-chance swings, once Pulse is on
  - The prompt says to answer only from tool results and to say "I don't have that" otherwise.

**Dependencies on the main app.** v0 and v1 need only routes that already exist. Better casts need two things:
- the narrative log served from the API (a `GET .../log` route; the log key is already settled under `parsed/{id}/{parse_id}/log/`);
- Pulse switched on in prod.

## Architecture

```text
Discord ── gateway (WSS) ──┐
Discord voice (UDP, DAVE) ─┤
                           ▼
                  discord-bot (Node, one process)
                    ├─ demoView Worker API (HTTPS, read-only)
                    ├─ Gemini TTS (HTTPS)          v1
                    └─ Gemini Live (WSS)           v2
```

**This can't run on Cloudflare.** Two hard constraints:

1. **Discord voice is UDP.** Voice media is RTP over UDP to Discord's voice servers. Workers have no UDP. Containers allow outbound traffic only on ports 80 and 443, plus DNS. So the voice process needs a normal host.
2. **DAVE is mandatory.** Since March 1, 2026, apps that don't speak Discord's end-to-end encryption protocol (DAVE) can't join voice calls, except in stage channels. `@discordjs/voice` supports it through `@snazzah/davey`. That's the main reason to use Node and not Go here: Go's Discord libraries' DAVE support is unproven.

**Proposed host:** one small always-on VM, e.g. a Fly.io `shared-cpu-1x` at about $2–5 a month or any $5 VPS. This departs from the project's "everything on Cloudflare" setup, so it needs your OK. A home machine would also work, but it breaks the spec's "prod runs off-machine" principle.

**Alternative (v0 only):** the v0 slash commands could run as an HTTP interactions endpoint on the existing Worker, with no new host. But once an interactions URL is set, Discord sends commands there and not to the gateway. The Worker would then have to relay voice commands to the VM, which means two moving parts. So: a single Node gateway process from day one, unless you want v0 shipped before any VM exists.

**Audio plumbing:**
- Discord sends and expects 48 kHz stereo Opus. Live wants 16 kHz mono PCM in and gives 24 kHz mono PCM out. TTS returns WAV or L16.
- The bot resamples both ways, with `prism-media` and a small resampler. No ffmpeg is needed for PCM.

**v2 specifics:**
- Subscribe only to the invoking user's audio stream, using the speaking events to bracket utterances.
- Send audio to Live as it arrives, and play its audio back as it streams.
- If the user talks over it, stop playback (barge-in).
- A Live connection is time-limited. Plan for session resumption, and cap sessions at 15 minutes either way.

## Stack

- TypeScript on Node 22, to match `worker/` and `web/`.
- `discord.js`, `@discordjs/voice`, `@snazzah/davey`, `prism-media`.
- `@google/genai` for both TTS and Live.
- vitest for tests. oxlint for linting, as in the other packages.
- Config comes from env vars: `DISCORD_TOKEN`, `DISCORD_APP_ID`, `GEMINI_API_KEY`, `DEMOVIEW_API` (dev or prod base URL), and `GUILD_ALLOWLIST`.

### Folder layout (proposed)

```text
discord-bot/
  SCOPE.md            this file
  package.json
  src/
    index.ts          gateway client, command registration
    commands/         match, round, recent, cast, stop, ask
    api.ts            typed demoView API client, reusing schema/ types
    cast/script.ts    match.json + round file (+ pulse, log) -> cast script
    voice/tts.ts      Gemini TTS -> PCM -> Discord player
    voice/live.ts     v2: Live session, tools, barge-in
    voice/audio.ts    resampling, Opus encode/decode
  test/               script builder golden tests on schema/samples
  Dockerfile          for the VM
```

## Milestones

| Milestone | Work | Exit check |
| --- | --- | --- |
| **B0: scaffold** | Bot app in the Discord developer portal, `discord-bot/` package, the typed API client, `/match`, `/round` and `/recent`, run locally against dev. | `/match` on a dev match posts the right score and a working link. |
| **B1: voice out** | Join and leave with DAVE, play a fixed WAV, `/stop`. | The bot speaks a test clip in an E2EE voice channel. |
| **B2: caster** | The script builder (golden-tested on `schema/samples`), Flash-Lite TTS, `/cast` and `/cast-match`. | `/cast` reads a round from one of your matches, and every fact in it matches the viewer. |
| **B3: host** | Dockerfile, deploy to the VM, secrets, a restart policy, and one JSON log line per command. | It runs a week on the VM without a manual restart. |
| **B4: co-caster** | `gemini-3.8-live` with tools, push-to-talk from the invoker only, barge-in, session caps. | 10 scripted questions about one match: correct answers to at least 9, and "I don't know" for the rest, never a made-up fact. |

B0 to B3 is about a weekend at the project's agent pace, and B4 another one. B4 waits on the narrative log route for its richer answers.

## Out of scope

- Uploading demos from Discord.
- Listening to anyone other than the person who ran `/ask`, or to anyone outside an active `/ask` session. No recording, and no transcripts kept.
- Translation, and casting in other languages, at first. Flash-Lite TTS supports 100+ languages, so this is a later toggle and not a separate model.
- Live game integration of any kind. The input stays parsed demo files (the CLAUDE.md hard boundary).
- Posting to any guild not on the allowlist, at first.

## Decisions for you

1. **Host:** a small VM off Cloudflare (Fly.io or a VPS) for the voice process. *Proposed: Fly.io.*
2. **Single process vs. Worker interactions:** *Proposed: a single Node gateway process.*
3. **Voice tiering:** Flash-Lite TTS for casts and 3.8 Live for Q&A, not Live for everything. *Proposed: as listed.*
4. **Privacy for `/ask`:** only the invoker, push-to-talk, nothing stored. *Proposed: as listed. A short "listening" notice posts in the channel while a session is open.*
5. **API target:** dev only until B3, then prod read-only.
6. **Branch:** this file sits on `claude/wizardly-einstein-r640sy` for now. Move it to its own `discord-bot` branch when you make one.

## Sources

- Gemini models list (3.8 Live, 3.8 TTS, 3.5 Live Translate): https://ai.google.dev/gemini-api/docs/models
- Gemini pricing: https://ai.google.dev/gemini-api/docs/pricing
- Live API audio formats: https://ai.google.dev/gemini-api/docs/live
- Discord DAVE requirement: https://discord.com/blog/every-voice-and-video-call-on-discord-is-now-end-to-end-encrypted
- Cloudflare Containers outbound traffic: https://developers.cloudflare.com/containers/platform-details/outbound-traffic
