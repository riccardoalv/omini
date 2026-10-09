import { computed, ref, type Ref } from 'vue'

import { childrenOf, type GraphView } from '@/lib/graph'
import { prefs } from '@/lib/prefs'
import type { TopoEdge, TopoNode } from '@/lib/types'

/**
 * Folding and unfolding on the map: a node's children in a bubble, areas in
 * one, and the node menu (right click) that does it. The choices live in the
 * user's preferences.
 */
export function useFolding(
  view: Ref<GraphView>,
  mapGraph: Ref<{ nodes: TopoNode[]; edges: TopoEdge[] }>,
  selectedId: Ref<string | undefined>,
) {
  let anchor: string | undefined // node the user expanded or collapsed

  /** The node last expanded or collapsed (the layout keeps it still), once. */
  function takeAnchor() {
    const id = anchor
    anchor = undefined
    return id
  }

  /**
   * Shows a node's clients. The side panel only follows when the expansion came
   * from it (its "Show all" button); a middle click or the menu leave it alone.
   */
  function expand(parentId: string, fromPanel = false) {
    anchor = parentId
    prefs.collapsed = prefs.collapsed.filter((id) => id !== parentId)
    if (!prefs.expanded.includes(parentId)) prefs.expanded.push(parentId)
    if (fromPanel) selectedId.value = parentId
  }

  function collapse(parentId: string) {
    anchor = parentId
    prefs.expanded = prefs.expanded.filter((id) => id !== parentId)
    if (!prefs.collapsed.includes(parentId)) prefs.collapsed.push(parentId)
  }

  function collapseArea(id: number) {
    if (!prefs.collapsedAreas.includes(id)) prefs.collapsedAreas.push(id)
  }
  function expandArea(id: number) {
    prefs.collapsedAreas = prefs.collapsedAreas.filter((x) => x !== id)
    selectedId.value = undefined
  }

  /** Expands the children of a node if they are grouped, groups them otherwise. */
  function toggleChildren(id: string) {
    if (id.startsWith('area-bubble:')) return expandArea(Number(id.slice(12)))
    const parent = view.value.groups.find((g) => g.id === id)?.parentId ?? id
    if (view.value.groups.some((g) => g.parentId === parent)) expand(parent)
    // Counted on the map's graph: a Wi-Fi network's clients are its children there.
    else if (childrenOf(parent, mapGraph.value.nodes, mapGraph.value.edges).length > 0)
      collapse(parent)
  }

  // Context menu (right click on a node).
  const menu = ref<{ x: number; y: number; id: string }>()

  /** The parent whose clients a menu action applies to (a group acts for its parent). */
  const menuParent = computed(() => {
    const id = menu.value?.id
    if (!id) return undefined
    return view.value.groups.find((g) => g.id === id)?.parentId ?? id
  })
  const menuCanCollapse = computed(() => {
    const id = menuParent.value
    // Possible unless everything is already collapsed by the user (a partial
    // automatic group can still be extended to all clients).
    return (
      !!id &&
      !prefs.collapsed.includes(id) &&
      childrenOf(id, mapGraph.value.nodes, mapGraph.value.edges).length > 0
    )
  })
  const menuCanExpand = computed(() => {
    const id = menuParent.value
    return !!id && view.value.groups.some((g) => g.parentId === id)
  })

  function menuAction(action: 'collapse' | 'expand' | 'details') {
    const id = menuParent.value
    const target = menu.value?.id
    menu.value = undefined
    if (!id) return
    if (action === 'collapse') collapse(id)
    else if (action === 'expand') expand(id)
    else selectedId.value = target
  }

  return {
    takeAnchor,
    expand,
    collapse,
    collapseArea,
    expandArea,
    toggleChildren,
    menu,
    menuCanCollapse,
    menuCanExpand,
    menuAction,
  }
}
