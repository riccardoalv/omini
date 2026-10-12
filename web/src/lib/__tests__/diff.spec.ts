import { describe, expect, it } from 'vitest'

import { diffLines, diffParts } from '../diff'

describe('diffLines', () => {
  it('finds the lines removed and added', () => {
    const got = diffLines('a\nb\nc\nd', 'a\nB\nc\nd\ne')
    expect(got.map((l) => l.op + l.text)).toEqual([' a', '-b', '+B', ' c', ' d', '+e'])
  })

  it('equal texts have no change', () => {
    expect(diffLines('x\ny', 'x\ny').every((l) => l.op === ' ')).toBe(true)
  })
})

describe('diffParts', () => {
  it('keeps context around changes and skips the rest', () => {
    const a = Array.from({ length: 20 }, (_, i) => `line ${i}`).join('\n')
    const b = a.replace('line 10', 'line ten')
    const parts = diffParts(diffLines(a, b), 2)
    expect(parts).toHaveLength(3)
    expect(parts[0]).toEqual({ skipped: 8 })
    expect('lines' in parts[1]! && parts[1].lines.map((l) => l.op + l.text)).toEqual([
      ' line 8',
      ' line 9',
      '-line 10',
      '+line ten',
      ' line 11',
      ' line 12',
    ])
    expect(parts[2]).toEqual({ skipped: 7 })
  })
})
