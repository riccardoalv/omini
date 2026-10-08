import { reactive } from 'vue'

/** Screen state shared by components that do not know each other. */
export const ui = reactive({
  /** Width of the device panel open on the map (0: closed): the popups stay clear of it. */
  panelWidth: 0,
})
