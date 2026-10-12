/** A line of a difference: kept (' '), removed ('-') or added ('+'). */
export interface DiffLine {
  op: ' ' | '-' | '+'
  text: string
}

/** A run of lines to show, or a gap of unchanged lines left out. */
export type DiffPart = { lines: DiffLine[] } | { skipped: number }

/** Past this many cells the middle is shown as all removed then all added. */
const MAX_CELLS = 4_000_000

/**
 * The line differences between two texts (the longest common subsequence of
 * what differs once the common start and end are set aside).
 */
export function diffLines(a: string, b: string): DiffLine[] {
  const x = a.split('\n')
  const y = b.split('\n')
  let start = 0
  while (start < x.length && start < y.length && x[start] === y[start]) start++
  let end = 0
  while (
    end < x.length - start &&
    end < y.length - start &&
    x[x.length - 1 - end] === y[y.length - 1 - end]
  )
    end++
  const xs = x.slice(start, x.length - end)
  const ys = y.slice(start, y.length - end)
  const out: DiffLine[] = x.slice(0, start).map((text) => ({ op: ' ', text }))
  if (xs.length * ys.length > MAX_CELLS) {
    out.push(...xs.map((text) => ({ op: '-' as const, text })))
    out.push(...ys.map((text) => ({ op: '+' as const, text })))
  } else {
    // lcs[i][j]: common lines of xs[i:] and ys[j:].
    const w = ys.length + 1
    const lcs = new Uint32Array((xs.length + 1) * w)
    for (let i = xs.length - 1; i >= 0; i--)
      for (let j = ys.length - 1; j >= 0; j--)
        lcs[i * w + j] =
          xs[i] === ys[j]
            ? lcs[(i + 1) * w + j + 1]! + 1
            : Math.max(lcs[(i + 1) * w + j]!, lcs[i * w + j + 1]!)
    let i = 0
    let j = 0
    while (i < xs.length || j < ys.length) {
      if (i < xs.length && j < ys.length && xs[i] === ys[j]) {
        out.push({ op: ' ', text: xs[i]! })
        i++
        j++
      } else if (
        i < xs.length &&
        (j >= ys.length || lcs[(i + 1) * w + j]! >= lcs[i * w + j + 1]!)
      ) {
        out.push({ op: '-', text: xs[i]! })
        i++
      } else {
        out.push({ op: '+', text: ys[j]! })
        j++
      }
    }
  }
  out.push(...x.slice(x.length - end).map((text) => ({ op: ' ' as const, text })))
  return out
}

/** The changed lines with `context` unchanged lines around them; the rest skipped. */
export function diffParts(lines: DiffLine[], context = 3): DiffPart[] {
  const keep = lines.map(() => false)
  lines.forEach((l, i) => {
    if (l.op === ' ') return
    for (let k = Math.max(0, i - context); k <= Math.min(lines.length - 1, i + context); k++)
      keep[k] = true
  })
  const out: DiffPart[] = []
  let run: DiffLine[] = []
  let skipped = 0
  lines.forEach((l, i) => {
    if (keep[i]) {
      if (skipped) out.push({ skipped })
      skipped = 0
      run.push(l)
    } else {
      if (run.length) out.push({ lines: run })
      run = []
      skipped++
    }
  })
  if (run.length) out.push({ lines: run })
  if (skipped) out.push({ skipped })
  return out
}
