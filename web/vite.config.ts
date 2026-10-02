import preact from '@preact/preset-vite'
import { existsSync, readdirSync, readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { defineConfig, type Plugin } from 'vite'

const SAMPLES_DIR = fileURLToPath(new URL('../schema/samples/', import.meta.url))

// The sample files the viewer can serve: match.json and rounds/NN.json.
function sampleFiles(): string[] {
  if (!existsSync(SAMPLES_DIR + 'match.json')) return []
  const rounds = existsSync(SAMPLES_DIR + 'rounds')
    ? readdirSync(SAMPLES_DIR + 'rounds').filter((f) => /^\d+\.json$/.test(f))
    : []
  return ['match.json', ...rounds.map((f) => `rounds/${f}`)]
}

// Serves ../schema/samples at /samples/ in dev and emits the same files into
// the build, so fixtures mode (and the /m/sample match) works with no backend
// and without copying the samples into web/. __SAMPLE_ROUNDS__ lists the
// rounds that have a sample file.
function samples(): Plugin {
  return {
    name: 'demoview-samples',
    config() {
      const rounds = sampleFiles()
        .filter((f) => f.startsWith('rounds/'))
        .map((f) => parseInt(f.slice('rounds/'.length), 10))
        .sort((a, b) => a - b)
      return { define: { __SAMPLE_ROUNDS__: JSON.stringify(rounds) } }
    },
    configureServer(server) {
      server.middlewares.use('/samples', (req, res) => {
        const name = (req.url ?? '').split('?')[0].replace(/^\/+/, '')
        res.setHeader('content-type', 'application/json')
        // Only the listed files, so the path can't escape the directory.
        if (!sampleFiles().includes(name)) {
          res.statusCode = 404
          res.end(JSON.stringify({ error: 'no such sample file' }))
          return
        }
        res.end(readFileSync(SAMPLES_DIR + name))
      })
    },
    generateBundle() {
      for (const name of sampleFiles()) {
        this.emitFile({ type: 'asset', fileName: `samples/${name}`, source: readFileSync(SAMPLES_DIR + name) })
      }
    },
  }
}

// https://vite.dev/config/
export default defineConfig({
  plugins: [preact(), samples()],
  // One chunk and no dynamic imports, so the preload polyfill is dead weight.
  build: { modulePreload: { polyfill: false } },
})
