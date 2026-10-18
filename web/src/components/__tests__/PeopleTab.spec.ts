import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import type { InventoryEntry, Person } from '@/lib/types'

import PeopleTab from '../map/PeopleTab.vue'
import { plugins } from './helpers'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return {
    ...mod,
    api: {
      ...mod.api,
      people: vi.fn<typeof mod.api.people>(),
      inventory: vi.fn<typeof mod.api.inventory>(),
      createPerson: vi.fn<typeof mod.api.createPerson>(),
      updatePerson: vi.fn<typeof mod.api.updatePerson>(),
      deletePerson: vi.fn<typeof mod.api.deletePerson>(),
      pausePerson: vi.fn<typeof mod.api.pausePerson>(),
      resumePerson: vi.fn<typeof mod.api.resumePerson>(),
    },
  }
})

const ana: Person = {
  id: 1,
  name: 'Ana',
  created_at: '',
  devices: ['mac:phone', 'dev:fw'],
  home: true,
}
const bob: Person = { id: 2, name: 'Bob', created_at: '', devices: [], home: false }
const entry = (id: string, label: string, online = true): InventoryEntry => ({
  id,
  kind: 'client',
  label,
  hidden: false,
  first_seen: '',
  last_seen: '',
  online,
})

const mountIt = async (pausedIds: string[] = []) => {
  const w = mount(PeopleTab, { props: { pausedIds }, global: { plugins: plugins() } })
  await flushPromises()
  return w
}

describe('PeopleTab', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.people).mockResolvedValue([ana, bob])
    vi.mocked(api.inventory).mockResolvedValue([
      entry('mac:phone', 'Ana phone'),
      entry('dev:fw', 'Firewall'),
    ])
  })

  it('lists people with whether they are home and how many devices they have', async () => {
    const w = await mountIt()
    const rows = w.findAll('[data-test=person-open]')
    expect(rows.map((r) => r.text())).toEqual(['Ana2 devices', 'Bobno devices'])
    expect(rows[0]!.get('[data-test=home-dot]').classes()).toContain('online')
    expect(rows[1]!.get('[data-test=home-dot]').classes()).not.toContain('online')
  })

  it('says how to start when there is nobody', async () => {
    vi.mocked(api.people).mockResolvedValue([])
    const w = await mountIt()
    expect(w.find('[data-test=people-empty]').exists()).toBe(true)
  })

  it('adds, renames and deletes a person (after a confirmation)', async () => {
    vi.mocked(api.createPerson).mockResolvedValue({ ...bob, id: 3, name: 'Cid' })
    vi.mocked(api.updatePerson).mockResolvedValue({ ...ana, name: 'Ana Lima' })
    vi.mocked(api.deletePerson).mockResolvedValue()
    const w = await mountIt()
    await w.get('[data-test=person-name]').setValue('  Cid ')
    await w.get('form.add').trigger('submit')
    await flushPromises()
    expect(api.createPerson).toHaveBeenCalledWith({ name: 'Cid' })

    const first = () => w.get('[data-person="1"]')
    await first().get('[data-test=rename]').trigger('click')
    await first().get('[data-test=rename-input]').setValue('Ana Lima')
    await first().get('form.rename').trigger('submit')
    await flushPromises()
    expect(api.updatePerson).toHaveBeenCalledWith(1, { name: 'Ana Lima' })

    await first().get('[data-test=delete]').trigger('click')
    expect(api.deletePerson).not.toHaveBeenCalled()
    expect(first().get('[data-test=delete-confirm]').text()).toContain('Delete Ana?')
    await first().get('[data-test=confirm-delete]').trigger('click')
    await flushPromises()
    expect(api.deletePerson).toHaveBeenCalledWith(1)
    expect(w.emitted('changed')).toHaveLength(1)
  })

  it("opens a person's devices, and a click shows one on the map", async () => {
    const w = await mountIt()
    await w.get('[data-person="1"] [data-test=person-open]').trigger('click')
    const devices = w.findAll('[data-test=person-device]')
    expect(devices.map((d) => d.text())).toEqual(['Ana phone', 'Firewall'])
    await devices[0]!.trigger('click')
    expect(w.emitted('select')).toEqual([['mac:phone']])
  })

  it('pauses a person after one confirmation, and says what happened to each device', async () => {
    vi.mocked(api.pausePerson).mockResolvedValue([
      { node_id: 'dev:fw', status: 'skipped' },
      { node_id: 'mac:phone', status: 'done' },
    ])
    const w = await mountIt()
    await w.get('[data-person="1"] [data-test=person-open]').trigger('click')
    expect(w.find('[data-test=resume-person]').exists()).toBe(false) // nothing paused
    await w.get('[data-test=pause-person]').trigger('click')
    expect(api.pausePerson).not.toHaveBeenCalled()
    expect(w.get('[data-test=pause-person-confirm]').text()).toContain(
      'Pause the internet of every device of Ana?',
    )
    await w.get('[data-test=confirm-pause-person]').trigger('click')
    await flushPromises()
    expect(api.pausePerson).toHaveBeenCalledWith(1)
    const results = w.get('[data-test=person-results]').text()
    expect(results).toContain('Firewall: skipped')
    expect(results).toContain('Ana phone: done')
    expect(w.emitted('changed')).toHaveLength(1)
  })

  it('resumes a person with a paused device at once', async () => {
    vi.mocked(api.resumePerson).mockResolvedValue([
      { node_id: 'mac:phone', status: 'failed', error: 'router refused' },
    ])
    const w = await mountIt(['mac:phone', 'dev:fw'])
    await w.get('[data-person="1"] [data-test=person-open]').trigger('click')
    expect(w.find('[data-test=pause-person]').exists()).toBe(false) // all paused
    await w.get('[data-test=resume-person]').trigger('click')
    await flushPromises()
    expect(api.resumePerson).toHaveBeenCalledWith(1)
    expect(w.get('[data-test=person-results]').text()).toContain('failed (router refused)')
  })
})
