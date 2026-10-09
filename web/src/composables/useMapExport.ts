import { useVueFlow } from '@vue-flow/core'
import { ref, type Ref } from 'vue'
import { useI18n } from 'vue-i18n'

import type { ExportOptions } from '@/components/map/ExportDialog.vue'
import { formatSpeed } from '@/lib/format'
import type { GraphView } from '@/lib/graph'
import type { Direction } from '@/lib/layout'
import { displayName } from '@/lib/names'
import { applyTheme, prefs } from '@/lib/prefs'
import { NODE_SIZES } from '@/lib/sizes'
import type { LinkLabel } from '@/lib/traffic'
import type { MapArea, Point, TopologyResponse } from '@/lib/types'

/** The theme the screen shows now ("system" resolved). */
export function screenTheme(): 'dark' | 'light' {
  if (prefs.theme !== 'system') return prefs.theme
  return window.matchMedia?.('(prefers-color-scheme: light)').matches ? 'light' : 'dark'
}

/**
 * Export: the whole map as an image (PNG, SVG), a draw.io diagram or its data
 * (JSON). The code that builds the files (html-to-image, draw.io) is only
 * loaded when the user exports.
 */
export function useMapExport(options: {
  data: Ref<TopologyResponse | undefined>
  view: Ref<GraphView>
  positions: Ref<Record<string, Point>>
  visibleAreas: Ref<MapArea[]>
  linkLabels: Ref<Map<string, LinkLabel>>
  /** The map's element (its Vue Flow viewport is what an image shows). */
  mapEl: Ref<HTMLElement | undefined>
  /** Every group and area expanded, and the orientation, while an image is taken. */
  expandAll: Ref<boolean>
  exportDirection: Ref<Direction | undefined>
  /** Waits for the map to be laid out after a change (see useMapLayout). */
  untilLaidOut: (change: () => void) => Promise<void>
}) {
  const { data, view, positions, mapEl } = options
  const { t } = useI18n()
  const { getViewport, setViewport } = useVueFlow('omini-map')

  const exportOpen = ref(false)
  const exporting = ref(false)
  const exportError = ref('')

  /**
   * Exports the map. An image shows every group and area expanded, in the theme
   * and orientation picked: the map is laid out that way for the capture, then
   * put back as it was (theme, orientation, groups, zoom and position).
   */
  async function exportMap(choice: ExportOptions) {
    exportError.value = ''
    if (!data.value) return
    const { download, exportName, mapImage, mapJSON } = await import('@/lib/export')
    if (choice.format === 'json') {
      download(mapJSON(data.value), exportName('json'))
      exportOpen.value = false
      return
    }
    const viewportEl = mapEl.value?.querySelector<HTMLElement>('.vue-flow__viewport')
    if (!viewportEl) return
    exporting.value = true
    const screen = getViewport()
    const turned = choice.direction !== prefs.layoutDirection
    try {
      document.documentElement.dataset.theme = choice.theme
      mapEl.value?.classList.add('exporting')
      await options.untilLaidOut(() => {
        options.expandAll.value = true
        options.exportDirection.value = choice.direction
      })
      if (choice.format === 'drawio')
        download(await drawioFile(), exportName('drawio'), 'application/vnd.jgraph.mxfile')
      else {
        const background = getComputedStyle(mapEl.value!).backgroundColor
        download(await mapImage(choice.format, viewportEl, background), exportName(choice.format))
      }
      exportOpen.value = false
    } catch {
      exportError.value = t('map.exportFailed')
    } finally {
      applyTheme(prefs.theme)
      mapEl.value?.classList.remove('exporting')
      await options.untilLaidOut(() => {
        options.expandAll.value = false
        options.exportDirection.value = undefined
      })
      // Turning the map refits it: the screen goes back to where it was.
      if (turned) await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)))
      setViewport(screen)
      exporting.value = false
    }
  }

  /** The map as laid out now (expanded for the export) as a draw.io diagram. */
  async function drawioFile(): Promise<string> {
    const { mapDrawio } = await import('@/lib/drawio')
    const v = view.value
    const labels = options.linkLabels.value
    const nodes = [
      ...v.nodes.map((n) => ({
        id: n.id,
        label: displayName(n, t),
        sub: n.kind === 'ssid' ? undefined : n.ip,
        kind: n.kind,
        online: n.online,
        ...(positions.value[n.id] ?? { x: 0, y: 0 }),
        ...NODE_SIZES[n.kind]!,
      })),
      ...v.groups.map((g) => ({
        id: g.id,
        label: g.area?.name ?? t('map.groupLabel', { n: g.clients.length }),
        kind: 'group',
        ...(positions.value[g.id] ?? { x: 0, y: 0 }),
        ...NODE_SIZES.group!,
      })),
    ]
    const edges = v.edges.map((e) => {
      const l = labels.get(e.id)
      const label = l && !l.hidden ? [l.name, formatSpeed(l.speed)].filter(Boolean).join(' | ') : ''
      return {
        id: e.id,
        source: e.source,
        target: e.target,
        label,
        dashed: e.kind !== 'lldp' && e.kind !== 'fdb',
      }
    })
    const areaBoxes = options.visibleAreas.value.map((a) => ({
      id: String(a.id),
      name: a.name,
      color: a.color,
      x: a.x,
      y: a.y,
      width: a.width,
      height: a.height,
    }))
    return mapDrawio(nodes, edges, areaBoxes, 'Omini')
  }

  return { exportOpen, exporting, exportError, exportMap }
}
