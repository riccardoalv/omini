import { getRectOfNodes, type GraphNode } from '@vue-flow/core'
import { toPng, toSvg } from 'html-to-image'

import type { TopologyResponse } from './types'

export type ExportFormat = 'png' | 'svg' | 'json'

/** Margin around the map in an exported image. */
const PADDING = 48
/** Largest side of an exported image: big maps are scaled down to it. */
const MAX_SIDE = 8192

/**
 * Size and transform that fit every node in an image (the whole map, not just
 * what is on screen), at 1:1 when it fits within MAX_SIDE.
 */
export function exportFrame(nodes: GraphNode[]): {
  width: number
  height: number
  transform: string
} {
  const rect = getRectOfNodes(nodes)
  const scale = Math.min(
    1,
    MAX_SIDE / Math.max(rect.width + 2 * PADDING, rect.height + 2 * PADDING, 1),
  )
  const width = Math.ceil((rect.width + 2 * PADDING) * scale)
  const height = Math.ceil((rect.height + 2 * PADDING) * scale)
  const x = (PADDING - rect.x) * scale
  const y = (PADDING - rect.y) * scale
  return { width, height, transform: `translate(${x}px, ${y}px) scale(${scale})` }
}

/** SVG properties that come from style sheets: copied inline while exporting,
 * since an exported SVG does not carry the page's CSS (a wire's path would be
 * filled black, drawing triangles). */
const SVG_STYLE = [
  'fill',
  'stroke',
  'stroke-width',
  'stroke-dasharray',
  'stroke-linecap',
  'opacity',
]

/** Inlines the computed SVG styles under `root`; returns a function that undoes it. */
export function inlineSvgStyles(root: Element): () => void {
  const saved: [SVGElement, string | null][] = []
  for (const el of root.querySelectorAll<SVGElement>('svg *')) {
    saved.push([el, el.getAttribute('style')])
    const computed = getComputedStyle(el)
    for (const prop of SVG_STYLE) {
      const value = computed.getPropertyValue(prop)
      if (value) el.style.setProperty(prop, value)
    }
  }
  return () => {
    for (const [el, style] of saved) {
      if (style === null) el.removeAttribute('style')
      else el.setAttribute('style', style)
    }
  }
}

/** The map as an image (data URL): `viewport` is Vue Flow's viewport element. */
export async function mapImage(
  format: 'png' | 'svg',
  viewport: HTMLElement,
  nodes: GraphNode[],
  background: string,
): Promise<string> {
  const { width, height, transform } = exportFrame(nodes)
  const options = {
    backgroundColor: background,
    width,
    height,
    pixelRatio: format === 'png' ? 2 : 1, // sharp on high-density screens
    style: { width: `${width}px`, height: `${height}px`, transform },
  }
  const restore = inlineSvgStyles(viewport)
  try {
    return await (format === 'png' ? toPng(viewport, options) : toSvg(viewport, options))
  } finally {
    restore()
  }
}

/** The topology as JSON: nodes, links, areas and layout, as the API gives them. */
export function mapJSON(data: TopologyResponse): string {
  const { topology, areas, layout, generated_at } = data
  return JSON.stringify({ generated_at, topology, areas, layout }, null, 2)
}

/** "omini-map-2026-10-07.png" */
export function exportName(format: ExportFormat, now = new Date()): string {
  return `omini-map-${now.toISOString().slice(0, 10)}.${format}`
}

/** Saves a data URL or text as a file. */
export function download(content: string, filename: string, type = 'application/json') {
  const url = content.startsWith('data:')
    ? content
    : URL.createObjectURL(new Blob([content], { type }))
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  if (!content.startsWith('data:')) URL.revokeObjectURL(url)
}
