import { describe, expect, it } from 'vitest'

import { chartScale, chartSeries, niceMax, timeTicks } from '@/lib/chart'

const box = { width: 600, height: 180, left: 50, right: 10, top: 10, bottom: 20 }

describe('chart', () => {
  it('reads an interface from either end of the link', () => {
    const points = [{ at: '2026-10-07T10:00:00Z', rx_bps: 100, tx_bps: 5, rx_max_bps: 300 }]
    expect(chartSeries(points)[0]).toMatchObject({ down: 100, up: 5, downMax: 300, upMax: 5 })
    // The upstream port: what it sends is what the device downloads.
    expect(chartSeries(points, true)[0]).toMatchObject({ down: 5, up: 100, upMax: 300 })
  })

  it('rounds the top of the axis', () => {
    expect(niceMax(0)).toBe(1000)
    expect(niceMax(7_300_000)).toBe(10_000_000)
    expect(niceMax(1_200_000)).toBe(2_000_000)
    expect(niceMax(450)).toBe(500)
  })

  it('puts time ticks on round boundaries', () => {
    const from = Date.parse('2026-10-07T10:07:00Z')
    const ticks = timeTicks(from, from + 24 * 3600_000)
    expect(ticks.length).toBeGreaterThanOrEqual(3)
    expect(ticks.length).toBeLessThanOrEqual(5)
    for (const tick of ticks) expect(new Date(tick).getMinutes()).toBe(0)
  })

  it('breaks the line where data is missing', () => {
    const at = (min: number) => new Date(Date.parse('2026-10-07T10:00:00Z') + min * 60_000)
    const series = chartSeries(
      [0, 1, 2, 30, 31].map((m) => ({ at: at(m).toISOString(), rx_bps: 1e6, tx_bps: 1e5 })),
    )
    const s = chartScale(series, at(0).getTime(), at(31).getTime(), box)
    expect(s.downLine.match(/M/g)).toHaveLength(2) // two runs
    expect(s.downArea.match(/Z/g)).toHaveLength(2)
    expect(s.x(at(0).getTime())).toBe(box.left)
    expect(s.y(0)).toBe(box.height - box.bottom)
    expect(s.yTicks.map((tk) => tk.value)).toEqual([0, 500_000, 1_000_000])
  })
})
