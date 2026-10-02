import { describe, expect, it } from 'vitest'
import { placeLabels } from './labels'

const R = 6
const H = 12

describe('placeLabels', () => {
  it('puts a lone label to the right of its dot', () => {
    expect(placeLabels([{ x: 100, y: 100, width: 50 }], R, H)).toEqual([{ x: 109, y: 100, align: 'left' }])
  })

  it('leaves labels that do not touch on the right', () => {
    const spots = placeLabels(
      [
        { x: 100, y: 100, width: 50 },
        { x: 100, y: 200, width: 50 },
      ],
      R,
      H,
    )
    expect(spots.map((s) => s.align)).toEqual(['left', 'left'])
  })

  it('moves a label that would cover another to the left', () => {
    // The second dot's right-hand label would cover the first dot's label.
    const spots = placeLabels(
      [
        { x: 140, y: 100, width: 60 },
        { x: 100, y: 102, width: 60 },
      ],
      R,
      H,
    )
    expect(spots[0]).toEqual({ x: 149, y: 100, align: 'left' })
    expect(spots[1]).toEqual({ x: 91, y: 102, align: 'right' })
  })

  it('tries above and below when both sides are taken', () => {
    // The first two labels sit just right and just left of the third dot.
    const spots = placeLabels(
      [
        { x: 100, y: 100, width: 60 },
        { x: 82 - 60, y: 100, width: 60 },
        { x: 100, y: 100, width: 20 },
      ],
      R,
      H,
    )
    expect(spots[2].align).toBe('center')
    expect(spots[2].y).toBeLessThan(100)
  })

  it('falls back to the right when every side is taken', () => {
    const crowd = Array.from({ length: 6 }, () => ({ x: 100, y: 100, width: 40 }))
    expect(placeLabels(crowd, R, H).at(-1)).toEqual({ x: 109, y: 100, align: 'left' })
  })
})
