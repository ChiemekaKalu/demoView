// A tiny History API router: enough for three routes.

import { useEffect, useState } from 'preact/hooks'

const CHANGE = 'popstate'

export function navigate(to: string, opts: { replace?: boolean } = {}): void {
  if (opts.replace) history.replaceState(null, '', to)
  else history.pushState(null, '', to)
  dispatchEvent(new PopStateEvent(CHANGE))
  if (!opts.replace) scrollTo(0, 0)
}

// The current path. Changes to the query string alone (the replay's ?t=,
// written with replaceState) don't re-render.
export function usePath(): string {
  const [path, setPath] = useState(location.pathname)
  useEffect(() => {
    const onChange = () => setPath(location.pathname)
    addEventListener(CHANGE, onChange)
    return () => removeEventListener(CHANGE, onChange)
  }, [])
  return path
}

export function useTitle(title: string): void {
  useEffect(() => {
    document.title = title ? `${title} · demoView` : 'demoView'
  }, [title])
}
