import { useEffect, useState } from 'preact/hooks'
import { getGallery, getSampleCard, isAbort, SAMPLE_ID } from '../api'
import { Link } from '../Link'
import { useTitle } from '../router'
import { Gallery, type GalleryState } from './Gallery'
import { UploadCard } from './Upload'

export function Home() {
  useTitle('')
  const [gallery, setGallery] = useState<GalleryState>({ kind: 'loading' })
  useEffect(() => {
    const ctrl = new AbortController()
    const withSample = (kind: 'empty' | 'error') =>
      getSampleCard()
        .then((m) => [m], () => [])
        .then((matches) => ctrl.signal.aborted || setGallery({ kind, matches }))
    getGallery(ctrl.signal).then(
      (matches) => (matches.length ? setGallery({ kind: 'ready', matches }) : withSample('empty')),
      (err: unknown) => isAbort(err) || withSample('error'),
    )
    return () => ctrl.abort()
  }, [])

  return (
    <main class="page home">
      <header class="topbar">
        <span class="wordmark">demoView</span>
        <Link href={`/m/${SAMPLE_ID}`}>Sample match</Link>
      </header>
      <section class="hero">
        <div class="hero-text">
          <h1>CS2 demos as scoreboards and 2D replays.</h1>
          <p class="lede">Every featured match below opens to a full scoreboard and a scrubbable replay of each round.</p>
        </div>
        <UploadCard />
      </section>
      <Gallery state={gallery} />
    </main>
  )
}
