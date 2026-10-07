import type { TrafficPoint } from './types'

/** A chart point: down is towards the device the chart is about. */
export interface SeriesPoint {
  at: number
  down: number
  up: number
  downMax: number
  upMax: number
}

/**
 * The points of a traffic chart. An interface's received bytes are the
 * download, unless the chart looks at it from the other end of the link
 * (swap: what the port sends is what the device behind it downloads).
 */
export function chartSeries(points: TrafficPoint[], swap = false): SeriesPoint[] {
  return points.map((p) => {
    const rx = { avg: p.rx_bps, max: p.rx_max_bps ?? p.rx_bps }
    const tx = { avg: p.tx_bps, max: p.tx_max_bps ?? p.tx_bps }
    const [down, up] = swap ? [tx, rx] : [rx, tx]
    return { at: Date.parse(p.at), down: down.avg, up: up.avg, downMax: down.max, upMax: up.max }
  })
}

export interface Box {
  width: number
  height: number
  left: number
  right: number
  top: number
  bottom: number
}

/** A round top for the y axis: 1, 2 or 5 times a power of ten (in bit/s). */
export function niceMax(v: number): number {
  if (v <= 0) return 1000
  const pow = 10 ** Math.floor(Math.log10(v))
  for (const m of [1, 2, 5, 10]) if (v <= m * pow) return m * pow
  return 10 * pow
}

/** Evenly spaced times on round boundaries (hours, days, months) for the x axis. */
export function timeTicks(from: number, to: number, count = 5): number[] {
  const steps = [
    60_000,
    5 * 60_000,
    15 * 60_000,
    3600_000,
    3 * 3600_000,
    6 * 3600_000,
    12 * 3600_000,
    86400_000,
    7 * 86400_000,
    30 * 86400_000,
    91 * 86400_000,
  ]
  const step = steps.find((s) => (to - from) / s <= count) ?? steps[steps.length - 1]!
  const offset = new Date(from).getTimezoneOffset() * 60_000 // round in local time
  const out: number[] = []
  for (let t = Math.ceil((from - offset) / step) * step + offset; t <= to; t += step) out.push(t)
  return out
}

/**
 * Positions of a chart: the x and y scales, axis ticks and the SVG paths of
 * both directions. Lines break where data is missing (Omini was off, or the
 * device unreachable).
 */
export function chartScale(series: SeriesPoint[], from: number, to: number, box: Box) {
  const top = niceMax(Math.max(0, ...series.map((p) => Math.max(p.down, p.up))))
  const plotW = box.width - box.left - box.right
  const plotH = box.height - box.top - box.bottom
  const x = (at: number) => box.left + ((at - from) / Math.max(1, to - from)) * plotW
  const y = (v: number) => box.top + plotH - (Math.min(v, top) / top) * plotH
  const yTicks = [0, top / 2, top].map((value) => ({ value, y: y(value) }))
  const xTicks = timeTicks(from, to)

  // Gaps: more than three times the usual spacing between points.
  const spacing =
    series.length > 1
      ? Math.min(
          ...series
            .slice(1)
            .map((p, i) => p.at - series[i]!.at)
            .filter((d) => d > 0),
        )
      : Infinity
  const runs: SeriesPoint[][] = []
  for (const p of series) {
    const run = runs[runs.length - 1]
    const prev = run?.[run.length - 1]
    if (run && prev && p.at - prev.at <= spacing * 3) run.push(p)
    else runs.push([p])
  }
  const f = (n: number) => n.toFixed(1)
  const line = (k: 'down' | 'up') =>
    runs
      .map((r) => r.map((p, i) => `${i ? 'L' : 'M'}${f(x(p.at))},${f(y(p[k]))}`).join(''))
      .join('')
  const area = (k: 'down' | 'up') =>
    runs
      .filter((r) => r.length > 1)
      .map((r) => {
        const base = f(y(0))
        const pts = r.map((p) => `L${f(x(p.at))},${f(y(p[k]))}`).join('')
        return `M${f(x(r[0]!.at))},${base}${pts}L${f(x(r[r.length - 1]!.at))},${base}Z`
      })
      .join('')
  return {
    x,
    y,
    top,
    yTicks,
    xTicks,
    downLine: series.length ? line('down') : '',
    upLine: series.length ? line('up') : '',
    downArea: area('down'),
    upArea: area('up'),
  }
}
