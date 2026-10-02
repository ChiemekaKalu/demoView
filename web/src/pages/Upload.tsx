import { useRef, useState } from 'preact/hooks'
import { ApiError, FIXTURES, isAbort, SAMPLE_ID, uploadDemo, type UploadStage } from '../api'
import { Link } from '../Link'
import { navigate, useTitle } from '../router'

const MAX_BYTES = 1024 ** 3

const STAGE_TEXT: Record<UploadStage, string> = {
  creating: 'Creating the match…',
  uploading: 'Uploading…',
  confirming: 'Confirming the upload…',
}

export function Upload() {
  useTitle('')
  const [stage, setStage] = useState<UploadStage | null>(null)
  const [progress, setProgress] = useState<{ loaded: number; total: number } | null>(null)
  const [error, setError] = useState<string | null>(null)
  const ctrlRef = useRef<AbortController | null>(null)
  const busy = stage !== null

  const onFile = (e: Event) => {
    const f = (e.currentTarget as HTMLInputElement).files?.[0]
    setError(f && f.size > MAX_BYTES ? 'This file is larger than 1 GB, the upload limit.' : null)
  }

  const onSubmit = async (e: Event) => {
    e.preventDefault()
    const form = e.currentTarget as HTMLFormElement
    const token = (form.elements.namedItem('token') as HTMLInputElement).value.trim()
    // Read from the input, not state, so a just-picked file is never missed.
    const file = (form.elements.namedItem('demo') as HTMLInputElement).files?.[0]
    if (!file) return setError('Choose a .dem file first.')
    if (file.size > MAX_BYTES) return setError('This file is larger than 1 GB, the upload limit.')
    if (!token && !FIXTURES) return setError('Enter the upload token.')
    setError(null)
    setProgress(null)
    ctrlRef.current = new AbortController()
    try {
      const id = await uploadDemo(file, token, {
        onStage: setStage,
        onProgress: (loaded, total) => setProgress({ loaded, total }),
        signal: ctrlRef.current.signal,
      })
      navigate(`/m/${id}`)
    } catch (err) {
      setStage(null)
      if (isAbort(err)) return setError('Upload cancelled.')
      setError(uploadError(err))
    }
  }

  const pct = progress && progress.total > 0 ? Math.round((progress.loaded / progress.total) * 100) : 0
  return (
    <main class="page narrow">
      <h1>demoView</h1>
      <p>Upload a CS2 demo to get a scoreboard and a 2D replay of every round.</p>
      {FIXTURES && (
        <p class="notice">
          Fixtures mode: nothing is uploaded. Submitting opens the sample match.
        </p>
      )}
      <form class="upload" onSubmit={onSubmit} noValidate>
        <label for="demo">Demo file (.dem)</label>
        <input id="demo" name="demo" type="file" accept=".dem" required disabled={busy} onChange={onFile} />

        <label for="token">Upload token</label>
        <input
          id="token"
          name="token"
          type="password"
          autocomplete="current-password"
          required={!FIXTURES}
          disabled={busy}
          aria-describedby="token-help"
        />
        <p id="token-help" class="muted small">
          Uploads need the token. Viewing a match only needs its link.
        </p>

        <div class="actions">
          <button type="submit" class="primary" disabled={busy}>
            Upload
          </button>
          {busy && (
            <button type="button" onClick={() => ctrlRef.current?.abort()}>
              Cancel
            </button>
          )}
        </div>

        {busy && (
          <div class="progress" role="status" aria-live="polite">
            <label for="progress">
              {STAGE_TEXT[stage]}
              {stage === 'uploading' && progress && ` ${pct}% (${mb(progress.loaded)} of ${mb(progress.total)})`}
            </label>
            {/* Indeterminate (no value) until bytes are moving. Keyed so the value attribute isn't carried over. */}
            {stage === 'uploading' ? (
              <progress key="bytes" id="progress" max={100} value={pct} />
            ) : (
              <progress key="busy" id="progress" />
            )}
          </div>
        )}
        {error && (
          <p class="error" role="alert">
            {error}
          </p>
        )}
      </form>
      <p>
        Or open the <Link href={`/m/${SAMPLE_ID}`}>sample match</Link>.
      </p>
    </main>
  )
}

function mb(bytes: number): string {
  return `${(bytes / 1024 ** 2).toFixed(1)} MB`
}

function uploadError(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 401) return 'The upload token was not accepted.'
    if (err.status === 413) return 'The demo is larger than 1 GB.'
    return err.message
  }
  return err instanceof Error ? err.message : String(err)
}
