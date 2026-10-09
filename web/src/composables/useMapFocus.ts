import { useVueFlow } from '@vue-flow/core'
import { ref, type Ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import type { GraphView } from '@/lib/graph'
import type { TopoNode } from '@/lib/types'

/**
 * Opening a node from elsewhere: ?node=<id> (from the Alerts screen) and the
 * device list, a drawer on the map ("N devices"; /devices opens it too).
 */
export function useMapFocus(options: {
  nodeById: Ref<Map<string, TopoNode>>
  view: Ref<GraphView>
  selectedId: Ref<string | undefined>
  /** Unfolds a node's children (see useFolding). */
  expand: (parentId: string) => void
}) {
  const { nodeById, selectedId } = options
  const route = useRoute()
  const router = useRouter()
  const { fitView } = useVueFlow('omini-map')

  /**
   * ?node=<id> (from the Insights screen) opens that node's panel and centers
   * it, expanding the group it is folded into.
   */
  let focused: string | undefined
  function focusRequested() {
    const id = typeof route.query.node === 'string' ? route.query.node : undefined
    if (!id || id === focused || !nodeById.value.has(id)) return
    focusNode(id)
  }

  // The device list: a drawer on the map ("N devices"); /devices opens it too.
  const devicesOpen = ref(route.query.devices === '1')
  if (devicesOpen.value) void router.replace({ query: { ...route.query, devices: undefined } })
  function showDevice(id: string) {
    devicesOpen.value = false
    focused = undefined
    focusNode(id)
  }

  /** Opens a node's panel and centers the map on it (its group expanded). */
  function focusNode(id: string) {
    if (!nodeById.value.has(id)) return
    focused = id
    const node = nodeById.value.get(id)!
    if (node.parent_id && !options.view.value.nodes.some((n) => n.id === id))
      options.expand(node.parent_id)
    selectedId.value = id
    void router.replace({ query: { ...route.query, node: undefined } })
    setTimeout(() => {
      void fitView({ nodes: [id], maxZoom: 1.1, duration: 400, padding: 0.4 })
    }, 600)
  }

  return { devicesOpen, showDevice, focusRequested }
}
