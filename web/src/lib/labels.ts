// Where each player's name goes: beside its dot, on the first side (right,
// left, above, below) that doesn't cover a name already placed. Coordinates
// are canvas pixels.

export interface LabelSpot {
  x: number
  y: number
  align: CanvasTextAlign
}

interface Box {
  left: number
  top: number
  right: number
  bottom: number
}

export function placeLabels(
  dots: readonly { x: number; y: number; width: number }[],
  radius: number,
  height: number,
): LabelSpot[] {
  const gap = radius + 3
  const half = height / 2
  const placed: Box[] = []
  return dots.map(({ x, y, width }) => {
    const options: [LabelSpot, Box][] = [
      [{ x: x + gap, y, align: 'left' }, { left: x + gap, top: y - half, right: x + gap + width, bottom: y + half }],
      [{ x: x - gap, y, align: 'right' }, { left: x - gap - width, top: y - half, right: x - gap, bottom: y + half }],
      [
        { x, y: y - gap - half, align: 'center' },
        { left: x - width / 2, top: y - gap - height, right: x + width / 2, bottom: y - gap },
      ],
      [
        { x, y: y + gap + half, align: 'center' },
        { left: x - width / 2, top: y + gap, right: x + width / 2, bottom: y + gap + height },
      ],
    ]
    const [spot, box] = options.find(([, b]) => !placed.some((p) => overlaps(p, b))) ?? options[0]
    placed.push(box)
    return spot
  })
}

function overlaps(a: Box, b: Box): boolean {
  return a.left < b.right && b.left < a.right && a.top < b.bottom && b.top < a.bottom
}
