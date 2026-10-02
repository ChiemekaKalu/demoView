// Interactive explainers. A page mounts one with `<!-- widget: name -->`, and
// the Lab page lists them all.

export interface Widget {
  name: string
  title: string
  blurb: string
  page: string // the wiki page it belongs to
  load: () => Promise<{ mount: (root: HTMLElement) => void }>
}

export const widgets: Widget[] = [
  {
    name: 'layers',
    title: 'Demo file layers',
    blurb: 'Walk from the first bytes of a .dem file down to a single game event.',
    page: 'demo-format',
    load: () => import('./layers'),
  },
  {
    name: 'roundsim',
    title: 'Round outcome inference',
    blurb: 'Step through four rounds and see which event decides each one, using the inference rules in the parser.',
    page: 'match-logic',
    load: () => import('./roundsim'),
  },
  {
    name: 'guard',
    title: 'Consistency guard',
    blurb: 'Toggle whether our parse agrees with the oracle and see which match.json is published.',
    page: 'parser',
    load: () => import('./guard'),
  },
  {
    name: 'status',
    title: 'Match status machine',
    blurb: 'Step a match through upload, queue, parse and retry, including the dead letter queue.',
    page: 'worker',
    load: () => import('./status'),
  },
  {
    name: 'ticks',
    title: 'Ticks, frames and time',
    blurb: 'Convert between demo ticks, round-file frames and playback time using the schema formulas.',
    page: 'schema',
    load: () => import('./ticks'),
  },
  {
    name: 'radar',
    title: 'Radar coordinates',
    blurb: 'Hover the Mirage radar to read game coordinates, or enter coordinates to see where they land.',
    page: 'viewer',
    load: () => import('./radar'),
  },
  {
    name: 'interp',
    title: 'Frame interpolation',
    blurb: 'Why yaw is interpolated along the shorter arc, and how round-file frames relate to screen refreshes.',
    page: 'viewer',
    load: () => import('./interp'),
  },
]

export const widgetByName = (name: string): Widget | undefined => widgets.find((w) => w.name === name)

// Loads and mounts a widget into root.
export async function mountWidget(w: Widget, root: HTMLElement): Promise<void> {
  const mod = await w.load()
  mod.mount(root)
}
