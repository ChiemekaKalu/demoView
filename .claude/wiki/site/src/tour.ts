// The guided tour: one demo, followed from upload to replay.
// `read` links go to wiki pages (page#heading).

import type { Highlight } from './diagram'

export interface Step {
  title: string
  summary: string
  active: Highlight
  body: string
  facts?: string[]
  widget?: string
  read: [label: string, to: string][]
}

export const STEPS: Step[] = [
  {
    title: 'Upload a demo',
    summary: 'A server-recorded .dem file and one upload token.',
    active: { nodes: ['browser'], edges: [] },
    body: 'The input is a `.dem` file: a recording of everything a CS2 game server sent to its players, in order. Only server-recorded demos are supported. The upload form takes the file and an upload token, a single secret that keeps the bucket from filling with unrelated files.',
    facts: ['A full match is typically hundreds of MB.', 'The only game input is a demo file. Nothing in this project reads from a running game.'],
    read: [['Inside a CS2 demo', 'demo-format']],
  },
  {
    title: 'Direct upload to R2',
    summary: 'The Worker signs the request. The browser sends the bytes.',
    active: { nodes: ['browser', 'worker', 'r2'], edges: ['b-w', 'b-r2'] },
    body: 'The browser asks the Worker for a match id and a presigned upload URL, then sends the file straight to R2. The Worker never handles the bytes, because a Worker is limited to 128 MB of memory.',
    facts: ['The request carries `Authorization: Bearer <token>`.', 'The presigned URL expires, so the browser uses it right away.'],
    read: [['One upload, end to end', 'big-picture#one-upload-end-to-end'], ['The Worker', 'worker']],
  },
  {
    title: 'Confirm and enqueue',
    summary: 'The Worker verifies the object, marks it queued and enqueues a job.',
    active: { nodes: ['browser', 'worker', 'd1', 'queue'], edges: ['b-w', 'w-d1', 'w-q'] },
    body: 'When the upload finishes, the browser confirms it. The Worker checks that the object exists and is under 1 GB, records the match as `queued` in D1, and puts a job on the queue. The model below steps a match through its states.',
    widget: 'status',
    read: [['Match status', 'worker#match-status']],
  },
  {
    title: 'Queue consumer and parser call',
    summary: 'Retries, rejections and the dead letter queue.',
    active: { nodes: ['queue', 'worker', 'parser'], edges: ['w-q', 'w-p'] },
    body: 'A queue consumer in the same Worker takes the job, sets the match to `parsing` and calls the parser container. A 422 means the demo is invalid, so the match fails immediately. Any other failure is treated as transient: the job returns to the queue for up to three attempts, then moves to the dead letter queue.',
    facts: ['One job per batch, because a parse can take minutes.', 'A message for a match that has already finished is acknowledged and skipped, so redelivery is safe.'],
    read: [['The consumer', 'worker#the-consumer'], ['Parser runtime', 'infra#parser-runtime']],
  },
  {
    title: 'Reading the demo format',
    summary: 'From the file preamble down to a single game event.',
    active: { nodes: ['parser'], edges: [] },
    body: 'The parser reads the file as a stream and never holds it in memory. Each layer of the format contains the next, down to individual game events. The model below walks through the layers.',
    widget: 'layers',
    read: [['Inside a CS2 demo', 'demo-format'], ['The parser', 'parser']],
  },
  {
    title: 'Deriving rounds and winners',
    summary: 'Newer demos omit round results, so they are inferred.',
    active: { nodes: ['parser'], edges: [] },
    body: 'Newer demos do not record when a round ended or who won. Match logic derives both from the events that remain: kills, bomb events, surrender votes and the round clock. The model below shows which event decides each round.',
    widget: 'roundsim',
    read: [['Match logic', 'match-logic']],
  },
  {
    title: 'Reference round files and the guard',
    summary: 'Our match.json ships only when it agrees with the oracle.',
    active: { nodes: ['parser'], edges: [] },
    body: 'Until the parser decodes entities itself, round files come from a reference parser, the oracle. Our `match.json` is published only when it agrees with the oracle\'s, so the two files always describe the same match.',
    widget: 'guard',
    read: [['The consistency guard', 'parser#the-server-and-the-consistency-guard'], ['Testing', 'testing']],
  },
  {
    title: 'Persisting results',
    summary: 'A versioned prefix, a manifest, then database rows.',
    active: { nodes: ['parser', 'r2', 'worker', 'd1'], edges: ['w-p', 'p-r2', 'w-d1'] },
    body: 'The parser writes one file per round into its own prefix in R2, then `match.json`, then `manifest.json` last, so a manifest means the rest is in place. It returns `match.json` to the consumer, which saves players, rounds and kills to D1 and marks the match `ready` by moving a pointer to the new prefix in one batch.',
    facts: ['The parser never writes to D1. The Worker owns the database.', 'Each attempt has its own prefix, so a retry or reparse never overwrites a live parse.'],
    read: [['The schema', 'schema']],
  },
  {
    title: 'Rendering in the viewer',
    summary: 'Polling, cached round files, and interpolation between frames.',
    active: { nodes: ['browser', 'worker', 'r2', 'd1'], edges: ['b-w', 'w-r2', 'w-d1'] },
    body: 'The browser polls until the match is `ready`, then fetches round files, which are cached for a year. Data arrives at the round-file rate and the screen draws at its own, so the canvas interpolates between neighbouring frames.',
    widget: 'interp',
    read: [['The viewer', 'viewer']],
  },
  {
    title: 'Coordinates and the radar',
    summary: 'Three per-map constants convert game units to pixels.',
    active: { nodes: ['browser'], edges: [] },
    body: 'Positions are stored in game units. Three constants per map convert them to radar pixels. The model below shows both coordinate systems for the same point.',
    widget: 'radar',
    read: [['Map coordinates', 'viewer#map-coordinates'], ['Ticks, frames and time', 'schema']],
  },
  {
    title: 'Summary',
    summary: 'Upload, queue, parse, store, render.',
    active: { nodes: ['browser', 'worker', 'r2', 'd1', 'queue', 'parser'], edges: ['b-w', 'b-r2', 'w-r2', 'w-d1', 'w-q', 'w-p', 'p-r2'] },
    body: 'The path is upload, queue, parse, store and render. Everything else is detail about one of those arrows. The system map describes each component, the lab holds the remaining models, and the pages cover the detail. The Study tab has optional quizzes.',
    read: [['Decisions and their reasons', 'decisions']],
  },
]
