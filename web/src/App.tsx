import type { VNode } from 'preact'
import { Match } from './pages/Match'
import { Replay } from './pages/Replay'
import { Upload } from './pages/Upload'
import { Link } from './Link'
import { usePath, useTitle } from './router'

const routes: [RegExp, (m: RegExpExecArray) => VNode][] = [
  [/^\/$/, () => <Upload />],
  [/^\/m\/([^/]+)\/?$/, (m) => <Match id={decode(m[1])} />],
  [/^\/m\/([^/]+)\/r\/(\d+)\/?$/, (m) => <Replay id={decode(m[1])} n={Number(m[2])} />],
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
