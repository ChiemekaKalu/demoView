import { h } from '../dom'
import { diagram, NODES } from '../diagram'
import { windowCard } from '../ui'

interface Detail {
  role: string
  lives: string[]
  notes: string[]
  read: [label: string, to: string][]
}

const DETAILS: Record<string, Detail> = {
  browser: {
    role: 'A static Vite and Preact single-page app. It uploads a demo, polls for status and draws the replay on one canvas. It performs no analysis of its own.',
    lives: ['web/'],
    notes: ['Routes: upload `/`, match `/m/:id`, replay `/m/:id/r/:n`.', 'Served by the Worker from the same origin, so the API needs no CORS.', 'Runs against fixtures with no backend (`VITE_FIXTURES=1`).'],
    read: [['The viewer', 'viewer']],
  },
  worker: {
    role: 'One Cloudflare Worker with three jobs: the public API, the parse queue consumer, and hosting the viewer. It is the only component that talks to D1.',
    lives: ['worker/', 'worker/migrations/'],
    notes: ['Routes for create, confirm (which reuses a duplicate demo), status, match.json, round files and map radars.', 'The top of `wrangler.jsonc` is dev. Prod is `--env prod`.', 'Logs one JSON line per status change, keeps each match\'s history in D1, and reaches the parser through a `ParserContainer` binding.'],
    read: [['The Worker', 'worker'], ['Match status', 'worker#match-status']],
  },
  r2: {
    role: 'Object storage for raw demos, parsed files and map data. The browser uploads into it directly through a presigned URL.',
    lives: ['demos/{id}.dem', 'parsed/{id}/{parse_id}/manifest.json', 'parsed/{id}/{parse_id}/match.json', 'parsed/{id}/{parse_id}/rounds/NN.json', 'maps/'],
    notes: ['Buckets `demoview-dev` and `demoview-prod`, each with CORS for the browser PUT.', 'Raw demos are retained so a reparse never needs a new upload.', 'Map data is private because the source repository has no licence.'],
    read: [['R2 layout', 'big-picture#r2-layout']],
  },
  d1: {
    role: 'SQLite at Cloudflare, holding a queryable copy of each match summary: matches, players, rounds and kills.',
    lives: ['worker/migrations/0001_init.sql'],
    notes: ['Only the Worker reads or writes it. The parser never does.', 'Steam ids are stored but excluded from the public GET.', 'Timestamps are Unix milliseconds.'],
    read: [['Data', 'worker#data']],
  },
  queue: {
    role: 'A Cloudflare queue with a dead letter queue beside it. It decouples "upload confirmed" from "parse finished", which can take minutes.',
    lives: ['demoview-{env}-parse', 'demoview-{env}-parse-dlq'],
    notes: ['`max_retries: 2`, so three attempts, 30 s apart.', 'One job per batch.', 'The Worker also consumes the DLQ, to mark matches failed.'],
    read: [['The consumer', 'worker#the-consumer']],
  },
  parser: {
    role: 'The Go parser, written from scratch, and the server that wraps it. The oracle, a reference exporter built on demoinfocs, runs alongside it as the fallback. Prod ships our own round files.',
    lives: ['parser/', 'parser/cmd/server/', 'tools/oracle/'],
    notes: ['Streams the demo. Targets are under 512 MB and 60 s.', 'Runs as a Cloudflare Container with a private route that only the Worker can reach. Dev and prod both run it.', '`parser/` never imports the oracle.'],
    read: [['The parser', 'parser'], ['Match logic', 'match-logic'], ['Parser runtime', 'infra#parser-runtime']],
  },
}

const code = (s: string) => s.replace(/`([^`]+)`/g, '<code>$1</code>')

export function renderMap(root: HTMLElement, selected: string): void {
  const d = diagram((id) => { location.hash = `#/map/${id}` })
  const id = DETAILS[selected] ? selected : ''
  d.set({ nodes: id ? [id] : [], edges: [] })

  const detail = h('div', { class: 'map-detail' })
  if (id) {
    const info = DETAILS[id]
    const node = NODES.find((n) => n.id === id)!
    detail.append(
      h('div', { class: 'now' },
        h('div', { class: 'now-head' }, h('span', { class: 'now-title mono' }, node.title.toUpperCase()), h('span', { class: 'now-updated mono' }, node.sub)),
        h('div', { class: 'now-grid' },
          h('span', { class: 'now-label mono' }, 'role'), h('span', { class: 'now-value' }, info.role),
          h('span', { class: 'now-label mono' }, 'location'), h('span', { class: 'now-value mono small' }, ...info.lives.flatMap((l, k) => k ? [h('br'), l] : [l])),
          h('span', { class: 'now-label mono' }, 'notes'), h('ul', { class: 'now-value notes', html: info.notes.map((n) => `<li>${code(n)}</li>`).join('') }),
          h('span', { class: 'now-label mono' }, 'read'), h('span', { class: 'now-value read-links' }, ...info.read.map(([label, to]) => h('a', { class: 'pill', href: `#/p/${to.replace('#', '/')}` }, label))))))
  } else {
    detail.append(h('p', { class: 'muted' }, 'Select a component to see its role, where it lives in the repository, and what to read next.'))
  }

  root.replaceChildren(
    h('div', { class: 'crumb' }, h('span', { class: 'mono' }, 'Overview')),
    h('h1', { class: 'h1' }, 'System map'),
    h('p', { class: 'lede' }, 'Six components, and the arrows between them. Each one is described below.'),
    windowCard('system.svg', [d.el], 'select a component'),
    detail,
  )
}
