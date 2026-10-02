// What to tell the viewer when a match failed or can't be read.

// A failed match's error is "<code>: <detail>" (worker/src/matches.ts failure).
const FAILURES = new Map([
  [
    'bad_demo',
    "This file couldn't be read as a CS2 demo. Only complete, server-recorded CS2 demos work: not POV demos, CS:GO demos or files cut short.",
  ],
  ['demo_not_found', 'The demo never reached storage. Upload it again.'],
  ['too_large', 'The demo is larger than 1 GB, the upload limit.'],
  ['bad_output', 'The parser wrote something the site could not read. This is a bug on our side.'],
  ['parse_failed', 'The parser failed three times in a row. It may be a passing problem, so try uploading the demo again later.'],
])

// The message for a failed match, and the raw detail behind it when the code
// is known. Older matches and unknown codes show the error as it is.
export function explainFailure(error: string | null): { message: string; detail: string | null } {
  if (!error) return { message: 'No error message was recorded.', detail: null }
  const [, code = '', detail = null] = /^([a-z_]+): ([\s\S]*)$/.exec(error) ?? []
  const message = FAILURES.get(code)
  return message ? { message, detail } : { message: error, detail: null }
}

export const SCHEMA_VERSION = 1

// Null when this viewer reads the data format the files were written in. A
// newer format means the page is older than the site, so a reload fixes it.
export function schemaProblem(version: number | null | undefined): string | null {
  if (version == null || version === SCHEMA_VERSION) return null
  return `This match's data is in format ${version}, and this page reads format ${SCHEMA_VERSION}. Reload the page to get the newest version.`
}
