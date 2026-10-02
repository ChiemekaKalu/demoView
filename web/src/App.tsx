import type { ComponentType, VNode } from 'preact'
import { useEffect, useState } from 'preact/hooks'
import { Match } from './pages/Match'
import { Replay } from './pages/Replay'
import { Upload } from './pages/Upload'
import { Link } from './Link'
import { usePath, useTitle } from './router'

const routes: [RegExp, (m: RegExpExecArray) => VNode][] = [
  [/^\/$/, () => <Upload />],
  [/^\/m\/([^/]+)\/?$/, (m) => <Match id={decode(m[1])} />],
  [/^\/m\/([^/]+)\/r\/(\d+)\/?$/, (m) => <Replay id={decode(m[1])} n={Number(m[2])} />],
  [/^\/findings\/?$/, () => <Findings />],
]

function decode(s: string): string {
  try {
    return decodeURIComponent(s)
  } catch {
    return s
  }
}

export function App() {
  const path = usePath()
  for (const [pattern, render] of routes) {
    const m = pattern.exec(path)
    if (m) return render(m)
  }
  return <NotFound />
}

// The findings page carries its own data and styles, so it loads as a
// separate chunk the first time someone opens it, and replays don't pay for it.
function Findings() {
  const [Page, setPage] = useState<ComponentType | null>(null)
  useEffect(() => {
    import('./pages/Findings').then((m) => setPage(() => m.Findings))
  }, [])
  if (Page) return <Page />
  return (
    <main class="page">
      <p class="muted" role="status">
        Loading…
      </p>
    </main>
  )
}

function NotFound() {
  useTitle('Not found')
  return (
    <main class="page narrow">
      <h1>Page not found</h1>
      <p>
        <Link href="/">Go to the upload page</Link>
      </p>
    </main>
  )
}
