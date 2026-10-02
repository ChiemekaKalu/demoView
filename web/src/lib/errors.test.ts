import { describe, expect, it } from 'vitest'
import { explainFailure, schemaProblem } from './errors'

describe('explainFailure', () => {
  it('explains a known code and keeps the detail', () => {
    const { message, detail } = explainFailure('bad_demo: truncated: no Stop frame')
    expect(message).toMatch(/couldn't be read as a CS2 demo/)
    expect(detail).toBe('truncated: no Stop frame')
  })

  it('shows older and unknown errors as they are', () => {
    for (const error of ['parse failed after 3 attempts: parser returned 503', 'truncated: no Stop frame', 'constructor: x']) {
      expect(explainFailure(error)).toEqual({ message: error, detail: null })
    }
  })

  it('handles a missing error', () => {
    expect(explainFailure(null).message).toBe('No error message was recorded.')
  })
})

describe('schemaProblem', () => {
  it('accepts format 1, or no format yet', () => {
    expect(schemaProblem(1)).toBeNull()
    expect(schemaProblem(null)).toBeNull()
  })

  it('asks for a reload on any other format', () => {
    expect(schemaProblem(2)).toMatch(/format 2.*format 1.*Reload/)
  })
})
