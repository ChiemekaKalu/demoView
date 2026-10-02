# Learning center

A local site that presents the wiki pages in `..` as a guided walkthrough. It is not part of the product and nothing deploys it.

```sh
npm install
npm run dev      # http://localhost:5180, opens the browser
npm run build    # typecheck, then a static copy in dist/
```

Editing any `.md` in `..` reloads the browser. A new page appears in the Pages list on its own.

## Tabs

| Tab | What it is | Where it comes from |
| --- | ---------- | ------------------- |
| **Tour** | One demo followed through the system in 11 steps, with a live diagram and a model at the key steps | `src/tour.ts` |
| **Map** | The same diagram, selectable: what each component is, where it lives, what to read | `src/views/map.ts` |
| **Lab** | Seven interactive models | `src/widgets/` |
| **Pages** | The wiki itself, with search, section filters and a contents panel | the markdown files, rendered by `marked` and `mermaid` |
| **Study** | Optional: read-progress, notes, quizzes, flashcards | `localStorage`, `src/quizzes.ts`, the glossary |

Arrow keys move through the tour. `?theme=dark` or `?theme=light` previews a theme on one load.

## Design

The visual language is adapted from chiemeka.dev: its colour tokens (warm paper in light, dusk slate in dark, sage and tan accents), Newsreader for headings, IBM Plex Sans and Mono, hairline borders, window-style panels, pill controls and one centred column. The fonts are bundled from the `@fontsource` packages, so nothing loads from a CDN. The pixel landscapes, time-of-day sky and other playful touches from that site are deliberately left out.

## Adding things

**A tour step.** Add an entry to `STEPS` in `src/tour.ts`: a title, a one-line summary, which diagram parts to highlight, the text, an optional model by name, and links to pages (`page#heading`).

**A model.** Write `src/widgets/<name>.ts` exporting `mount(root: HTMLElement)`, and register it in `src/widgets/index.ts`. Use it in a tour step with `widget: '<name>'`, or in a page with `<!-- widget: <name> -->`. The comment is invisible on GitHub, so the markdown still reads normally there.

**A quiz question.** Add to the page's array in `src/quizzes.ts`. The first option is the correct one, because the quiz shuffles.

**A flashcard.** Add a `**Term.** Definition` line to `glossary.md`.

**The reading order.** `PATH` in `src/content.ts`.

## Notes

- The radar model reads `web/public/radar/de_mirage.png` from the repository, so it only works from a checkout. Without the file it draws a grid.
- Progress is stored per browser and per port. Keep using port 5180 so it persists.
- Dependencies are `marked`, `mermaid` and the three `@fontsource` packages. There is no framework.
