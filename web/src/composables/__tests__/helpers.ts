import { mount } from '@vue/test-utils'
import { defineComponent } from 'vue'

import { plugins } from '@/components/__tests__/helpers'
import type { MapArea, TopologyResponse, TopoNode } from '@/lib/types'

/** Runs a composable inside a mounted component (for useI18n, useVueFlow...). */
export function withSetup<T>(composable: () => T, extra: object[] = []) {
  let result!: T
  const wrapper = mount(
    defineComponent({
      setup() {
        result = composable()
        return () => null
      },
    }),
    { global: { plugins: [...plugins(), ...(extra as never[])] } },
  )
  return { result, wrapper }
}

/** A parent with `n` clients hanging from it. */
export function star(parent: string, n: number): TopologyResponse {
  const clients: TopoNode[] = Array.from({ length: n }, (_, i) => ({
    id: `mac:${i}`,
    kind: 'client',
    label: `phone-${i}`,
    online: true,
  }))
  return {
    topology: {
      nodes: [{ id: parent, kind: 'device', label: parent, online: true }, ...clients],
      edges: clients.map((c) => ({
        id: `e:${c.id}`,
        source: parent,
        target: c.id,
        kind: 'inferred',
      })),
    },
    statuses: [],
    generated_at: '2026-10-07T00:00:00Z',
    layout: {},
    areas: [],
  }
}

export const area = (id: number, members: string[], extra: Partial<MapArea> = {}): MapArea => ({
  id,
  name: `Area ${id}`,
  color: 'blue',
  direction: 'RIGHT',
  x: 0,
  y: 0,
  width: 100,
  height: 100,
  members,
  ...extra,
})
