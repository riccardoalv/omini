import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import ToggleSwitch from '../ToggleSwitch.vue'

describe('ToggleSwitch', () => {
  it('is an accessible switch reflecting its state', () => {
    const w = mount(ToggleSwitch, { props: { modelValue: true, label: 'Enabled' } })
    const btn = w.get('button')
    expect(btn.attributes('role')).toBe('switch')
    expect(btn.attributes('aria-checked')).toBe('true')
    expect(btn.attributes('aria-label')).toBe('Enabled')
    expect(btn.classes()).toContain('on')
  })

  it('emits the new value when clicked', async () => {
    const w = mount(ToggleSwitch, { props: { modelValue: false, label: 'Disabled' } })
    await w.get('button').trigger('click')
    expect(w.emitted('update:modelValue')).toEqual([[true]])
  })

  it('does nothing while disabled', async () => {
    const w = mount(ToggleSwitch, { props: { modelValue: false, label: 'x', disabled: true } })
    await w.get('button').trigger('click')
    expect(w.emitted('update:modelValue')).toBeUndefined()
  })
})
