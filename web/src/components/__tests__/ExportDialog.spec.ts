import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import ExportDialog from '../map/ExportDialog.vue'
import { plugins } from './helpers'

const mountDialog = () =>
  mount(ExportDialog, {
    props: { theme: 'dark', direction: 'RIGHT' },
    global: { plugins: plugins() },
    attachTo: document.body,
  })

describe('ExportDialog', () => {
  it("starts from the screen's theme and orientation, as a PNG", async () => {
    const w = mountDialog()
    await w.get('form').trigger('submit')
    expect(w.emitted('export')![0]).toEqual([{ format: 'png', theme: 'dark', direction: 'RIGHT' }])
    w.unmount()
  })

  it('exports with the options picked', async () => {
    const w = mountDialog()
    await w.get('[data-test=format-svg]').setValue(true)
    await w.get('[data-test=theme-light]').setValue(true)
    await w.get('[data-test=direction-DOWN]').setValue(true)
    await w.get('form').trigger('submit')
    expect(w.emitted('export')![0]).toEqual([{ format: 'svg', theme: 'light', direction: 'DOWN' }])
    w.unmount()
  })

  it('has no theme or orientation for the data (JSON)', async () => {
    const w = mountDialog()
    await w.get('[data-test=format-json]').setValue(true)
    const sets = w.findAll('fieldset')
    expect(sets[1]!.attributes('disabled')).toBeDefined()
    expect(sets[2]!.attributes('disabled')).toBeDefined()
    w.unmount()
  })
})
