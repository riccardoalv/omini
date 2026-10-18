import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api, ApiError } from '@/lib/api'

import NoteField from '../map/NoteField.vue'
import { plugins } from './helpers'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return { ...mod, api: { ...mod.api, updateInventory: vi.fn<typeof mod.api.updateInventory>() } }
})

const mountIt = (note?: string) =>
  mount(NoteField, { props: { nodeId: 'dev:nas', note }, global: { plugins: plugins() } })

describe('NoteField', () => {
  beforeEach(() => {
    vi.mocked(api.updateInventory)
      .mockReset()
      .mockResolvedValue({} as never)
  })

  it('saves the note when the field loses focus', async () => {
    const w = mountIt()
    const area = w.get('[data-test=note]')
    await area.setValue('  Living room, bought 2024 ')
    await area.trigger('blur')
    await flushPromises()
    expect(api.updateInventory).toHaveBeenCalledWith('dev:nas', {
      note: 'Living room, bought 2024',
    })
    expect(w.emitted('saved')![0]).toEqual(['Living room, bought 2024'])
  })

  it('does not save an unchanged note', async () => {
    const w = mountIt('Rack')
    await w.get('[data-test=note]').trigger('blur')
    await flushPromises()
    expect(api.updateInventory).not.toHaveBeenCalled()
  })

  it('clears a note', async () => {
    const w = mountIt('Rack')
    const area = w.get('[data-test=note]')
    await area.setValue('')
    await area.trigger('blur')
    await flushPromises()
    expect(api.updateInventory).toHaveBeenCalledWith('dev:nas', { note: '' })
  })

  it('shows the characters left near the limit', async () => {
    const w = mountIt()
    expect(w.find('[data-test=note-left]').exists()).toBe(false)
    await w.get('[data-test=note]').setValue('x'.repeat(950))
    expect(w.get('[data-test=note-left]').text()).toContain('50')
  })

  it('reports a refused save', async () => {
    vi.mocked(api.updateInventory).mockImplementation(async () => {
      throw new ApiError(400, 'the note is longer than 1000 characters')
    })
    const w = mountIt()
    const area = w.get('[data-test=note]')
    await area.setValue('x')
    await area.trigger('blur')
    await flushPromises()
    expect(w.emitted('error')![0]).toEqual(['the note is longer than 1000 characters'])
  })
})
