import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import OwnerField from '../map/OwnerField.vue'
import { plugins } from './helpers'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return {
    ...mod,
    api: {
      ...mod.api,
      people: vi.fn<typeof mod.api.people>(),
      createPerson: vi.fn<typeof mod.api.createPerson>(),
      updateInventory: vi.fn<typeof mod.api.updateInventory>(),
    },
  }
})

const mountIt = async (ownerId?: number) => {
  const w = mount(OwnerField, {
    props: { nodeId: 'mac:phone', ownerId },
    global: { plugins: plugins() },
  })
  await flushPromises()
  return w
}

describe('OwnerField', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.people).mockResolvedValue([
      { id: 1, name: 'Ana', created_at: '', devices: [], home: false },
    ])
    vi.mocked(api.updateInventory).mockResolvedValue({} as never)
  })

  it('offers nobody, the people and a new person, with the owner picked', async () => {
    const w = await mountIt(1)
    const select = w.get<HTMLSelectElement>('[data-test=owner]')
    expect(select.findAll('option').map((o) => o.text())).toEqual(['Nobody', 'Ana', 'New person…'])
    expect(select.element.value).toBe('1')
  })

  it('saves the owner as soon as one is picked, and nobody clears it', async () => {
    const w = await mountIt()
    await w.get('[data-test=owner]').setValue('1')
    await flushPromises()
    expect(api.updateInventory).toHaveBeenCalledWith('mac:phone', { owner_id: 1 })
    expect(w.emitted('saved')).toEqual([[1]])
    await w.setProps({ ownerId: 1 })
    await w.get('[data-test=owner]').setValue('')
    await flushPromises()
    expect(api.updateInventory).toHaveBeenLastCalledWith('mac:phone', { owner_id: null })
    expect(w.emitted('saved')![1]).toEqual([undefined])
  })

  it('creates a new person and makes them the owner', async () => {
    vi.mocked(api.createPerson).mockResolvedValue({
      id: 5,
      name: 'Bob',
      created_at: '',
      devices: [],
      home: false,
    })
    const w = await mountIt()
    await w.get('[data-test=owner]').setValue('new')
    expect(api.updateInventory).not.toHaveBeenCalled()
    await w.get('[data-test=owner-new-name]').setValue('Bob')
    await w.get('form.new').trigger('submit')
    await flushPromises()
    expect(api.createPerson).toHaveBeenCalledWith({ name: 'Bob' })
    expect(api.updateInventory).toHaveBeenCalledWith('mac:phone', { owner_id: 5 })
    expect(w.get<HTMLSelectElement>('[data-test=owner]').element.value).toBe('5')
  })
})
