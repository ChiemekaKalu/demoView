// The system as one picture. The tour lights up the parts a step is about, and
// the map lets you click them.

const NS = 'http://www.w3.org/2000/svg'

function s<K extends keyof SVGElementTagNameMap>(tag: K, attrs: Record<string, string | number> = {}, ...kids: (SVGElement | string)[]): SVGElementTagNameMap[K] {
  const el = document.createElementNS(NS, tag)
  for (const [k, v] of Object.entries(attrs)) el.setAttribute(k, String(v))
  for (const kid of kids) el.append(kid)
  return el
}

export interface NodeInfo {
  id: string
  title: string
  sub: string
  x: number
  y: number
}

const W = 146
const H = 48

export const NODES: NodeInfo[] = [
  { id: 'browser', title: 'Browser', sub: 'web/ · Preact', x: 20, y: 125 },
  { id: 'worker', title: 'Worker', sub: 'API · consumer', x: 250, y: 125 },
  { id: 'r2', title: 'R2', sub: 'demos · parsed', x: 520, y: 30 },
  { id: 'd1', title: 'D1', sub: 'match summaries', x: 520, y: 125 },
  { id: 'queue', title: 'Queue', sub: 'parse + DLQ', x: 250, y: 235 },
  { id: 'parser', title: 'Parser', sub: 'Go · oracle', x: 520, y: 235 },
]

interface Edge {
  id: string
  d: string
  label: string
  lx: number
  ly: number
  end?: boolean
}

const EDGES: Edge[] = [
  { id: 'b-w', d: 'M166 149 H250', label: 'API', lx: 208, ly: 141, end: true },
  { id: 'b-r2', d: 'M93 125 V46 H520', label: 'presigned PUT', lx: 300, ly: 38 },
  { id: 'w-r2', d: 'M323 125 V62 H520', label: 'serve files', lx: 420, ly: 78 },
  { id: 'w-d1', d: 'M396 140 H520', label: 'rows', lx: 458, ly: 132 },
  { id: 'w-q', d: 'M323 173 V235', label: 'enqueue', lx: 330, ly: 207 },
  { id: 'w-p', d: 'M396 165 H460 V259 H520', label: 'POST /parse', lx: 452, ly: 196, end: true },
  { id: 'p-r2', d: 'M666 259 H700 V54 H666', label: 'write files', lx: 694, ly: 206, end: true },
]

export interface Highlight {
  nodes: string[]
  edges: string[]
}

export interface Diagram {
  el: SVGSVGElement
  set(h: Highlight): void
}

export function diagram(onNode?: (id: string) => void): Diagram {
  const svg = s('svg', { viewBox: '0 0 720 300', class: 'diagram', role: 'img', 'aria-label': 'The demoView system: browser, Worker, R2, D1, queue and parser' })
  const defs = s('defs')
  for (const id of ['arrow', 'arrow-on']) {
    defs.append(s('marker', { id, viewBox: '0 0 10 10', refX: 9, refY: 5, markerWidth: 7, markerHeight: 7, orient: 'auto-start-reverse' }, s('path', { d: 'M0 0 L10 5 L0 10 z' })))
  }
  svg.append(defs)

  const edgeEls = new Map<string, SVGGElement>()
  for (const e of EDGES) {
    const g = s('g', { class: 'edge' }, s('path', { d: e.d, fill: 'none', 'marker-end': 'url(#arrow)' }), s('text', { x: e.lx, y: e.ly, ...(e.end ? { 'text-anchor': 'end' } : {}) }, e.label))
    edgeEls.set(e.id, g)
    svg.append(g)
  }

  const nodeEls = new Map<string, SVGGElement>()
  for (const n of NODES) {
    const g = s('g', { class: `node${onNode ? ' click' : ''}`, transform: `translate(${n.x} ${n.y})`, tabindex: onNode ? 0 : -1, role: onNode ? 'button' : 'img', 'aria-label': n.title },
      s('rect', { width: W, height: H, rx: 8 }),
      s('text', { x: W / 2, y: 21, class: 'node-title' }, n.title),
      s('text', { x: W / 2, y: 37, class: 'node-sub' }, n.sub))
    if (onNode) {
      g.addEventListener('click', () => onNode(n.id))
      g.addEventListener('keydown', (ev) => { if (ev.key === 'Enter' || ev.key === ' ') { ev.preventDefault(); onNode(n.id) } })
    }
    nodeEls.set(n.id, g)
    svg.append(g)
  }

  return {
    el: svg,
    set({ nodes, edges }) {
      for (const [id, g] of nodeEls) g.classList.toggle('on', nodes.includes(id))
      for (const [id, g] of edgeEls) {
        const on = edges.includes(id)
        g.classList.toggle('on', on)
        g.querySelector('path')!.setAttribute('marker-end', on ? 'url(#arrow-on)' : 'url(#arrow)')
      }
    },
  }
}
