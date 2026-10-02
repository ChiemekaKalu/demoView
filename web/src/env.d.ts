interface ImportMetaEnv {
  // API origin, e.g. https://api.example.com. Empty means same origin.
  readonly VITE_API_BASE?: string
  // "1" serves every match from schema/samples, with no backend.
  readonly VITE_FIXTURES?: string
}

// Round numbers that have a sample file, from vite.config.ts.
declare const __SAMPLE_ROUNDS__: number[]
