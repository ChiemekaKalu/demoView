import { describe, expect, it } from 'vitest'
import { galleryDate } from './format'

describe('galleryDate', () => {
  it("formats a timestamp as a medium date in the viewer's locale", () => {
    const ms = Date.UTC(2026, 9, 2, 12)
    expect(galleryDate(ms)).toBe(new Intl.DateTimeFormat(undefined, { dateStyle: 'medium' }).format(ms))
    expect(galleryDate(ms)).toMatch(/2026/)
  })
})
