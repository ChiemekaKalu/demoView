import { defineConfig } from 'vite'

// The pages are the markdown files one directory up, and the radar is in web/; the dev server must be
// allowed to read them. Editing one reloads the browser.
export default defineConfig({
  server: { port: 5180, fs: { allow: ['../../..'] } },
  preview: { port: 5180 },
})
