import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api, ApiError } from '@/lib/api'
import type { NetworkVlan } from '@/lib/types'

import VlanForm from '../map/VlanForm.vue'
import { plugins } from './helpers'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return {
    ...mod,
    api: {
      ...mod.api,
      createVlan: vi.fn<typeof mod.api.createVlan>(),
      updateVlan: vi.fn<typeof mod.api.updateVlan>(),
      setWifi: vi.fn<typeof mod.api.setWifi>(),
      deleteWifi: vi.fn<typeof mod.api.deleteWifi>(),
    },
  }
})

const lan: NetworkVlan = {
  id: 1,
  name: '',
  subnet: '192.168.1.0/24',
  devices: [],
  network: {
    vlan: 1,
    gateway: '192.168.1.1/24',
    dhcp: { enabled: true, from: '192.168.1.100', to: '192.168.1.199' },
    access: 'all',
  },
}
const form = (props: Partial<InstanceType<typeof VlanForm>['$props']> = {}) =>
  mount(VlanForm, {
    props: { vlans: [lan], router: true, allowDestructive: false, ...props },
    global: { plugins: plugins() },
  })
const value = (w: ReturnType<typeof form>, test: string) =>
  (w.get(`[data-test=${test}]`).element as HTMLInputElement).value

describe('VlanForm', () => {
  beforeEach(() => {
    vi.mocked(api.createVlan).mockReset()
    vi.mocked(api.updateVlan).mockReset()
    vi.mocked(api.setWifi).mockReset().mockResolvedValue({ changes: [] })
    vi.mocked(api.deleteWifi).mockReset().mockResolvedValue({ changes: [] })
  })

  it('suggests a free id, its gateway and a DHCP range, and follows the id', async () => {
    const w = form()
    expect(value(w, 'vlan-id')).toBe('10')
    expect(value(w, 'gateway')).toBe('192.168.10.1/24')
    expect([value(w, 'dhcp-from'), value(w, 'dhcp-to')]).toEqual([
      '192.168.10.100',
      '192.168.10.250',
    ])
    await w.get('[data-test=vlan-id]').setValue(30)
    expect(value(w, 'gateway')).toBe('192.168.30.1/24')
    expect(value(w, 'dhcp-from')).toBe('192.168.30.100')
  })

  it('creates the VLAN with its network, access "Internet only" by default', async () => {
    vi.mocked(api.createVlan).mockResolvedValue({
      vlan: { id: 10, name: 'IOT', devices: [] },
      changes: [],
    })
    const w = form()
    await w.get('[data-test=vlan-name]').setValue('IOT')
    await w.get('[data-access=local]').trigger('click')
    await w.get('[data-test=mdns]').trigger('click')
    await w.get('[data-test=vlan-form]').trigger('submit')
    await flushPromises()
    expect(api.createVlan).toHaveBeenCalledWith(
      expect.objectContaining({
        id: 10,
        name: 'IOT',
        confirm: false,
        network: {
          vlan: 10,
          gateway: '192.168.10.1/24',
          dhcp: { enabled: true, from: '192.168.10.100', to: '192.168.10.250' },
          access: 'local',
          mdns: true,
        },
      }),
    )
    expect(w.emitted('saved')).toHaveLength(1)
  })

  it('has no discovery toggle when the router has no mDNS repeater', async () => {
    vi.mocked(api.createVlan).mockResolvedValue({
      vlan: { id: 10, name: 'IOT', devices: [] },
      changes: [],
    })
    const w = form({ vlans: [{ ...lan, network: { ...lan.network!, mdns: null } }] })
    expect(w.find('[data-test=mdns]').exists()).toBe(false)
    await w.get('[data-test=vlan-name]').setValue('IOT')
    await w.get('[data-test=vlan-form]').trigger('submit')
    await flushPromises()
    expect(vi.mocked(api.createVlan).mock.calls[0]![0].network).not.toHaveProperty('mdns', false)
  })

  it('refuses a DHCP range outside the network', async () => {
    const w = form()
    await w.get('[data-test=dhcp-to]').setValue('192.168.11.20')
    expect(w.get('[role=alert]').text()).toContain('inside 192.168.10.0/24')
    expect(w.get('[data-test=vlan-save]').attributes('disabled')).toBeDefined()
  })

  it('without a router it is a VLAN with a subnet, as before', () => {
    const w = form({ router: false })
    expect(w.find('[data-test=vlan-network]').exists()).toBe(false)
  })

  it('warns that picking an access replaces rules set by hand', async () => {
    const custom = {
      ...lan,
      id: 20,
      network: { ...lan.network!, vlan: 20, access: 'custom' as const },
    }
    const w = form({ vlan: custom })
    expect(w.get('[data-access=custom]').attributes('aria-checked')).toBe('true')
    expect(w.find('[data-test=replaces-rules]').exists()).toBe(false)
    await w.get('[data-access=internet]').trigger('click')
    expect(w.find('[data-test=replaces-rules]').exists()).toBe(true)
  })

  it('asks again when the change cuts the network Omini is in', async () => {
    vi.mocked(api.updateVlan)
      .mockRejectedValueOnce(new ApiError(409, 'destructive'))
      .mockResolvedValueOnce({ vlan: lan, changes: [] })
    const w = form({ vlan: lan, allowDestructive: true })
    await w.get('[data-test=gateway]').setValue('192.168.2.1/24')
    await w.get('[data-test=vlan-form]').trigger('submit')
    await flushPromises()
    expect(w.find('[data-test=ask-again]').exists()).toBe(true)
    await w.get('[data-test=save-anyway]').trigger('click')
    await flushPromises()
    expect(vi.mocked(api.updateVlan).mock.calls[1]![1]).toMatchObject({ confirm: true })
    expect(w.emitted('saved')).toHaveLength(1)
  })

  it('says it is blocked when destructive changes are not allowed', async () => {
    vi.mocked(api.updateVlan).mockRejectedValue(new ApiError(409, 'destructive'))
    const w = form({ vlan: lan })
    await w.get('[data-test=vlan-form]').trigger('submit')
    await flushPromises()
    expect(w.find('[data-test=blocked]').exists()).toBe(true)
  })

  it('creates the VLAN with a Wi-Fi network named after it', async () => {
    vi.mocked(api.createVlan).mockResolvedValue({
      vlan: { id: 10, name: 'IOT', devices: [] },
      changes: [],
    })
    const w = form({ wifi: true })
    await w.get('[data-test=vlan-name]').setValue('IOT')
    await w.get('[data-test=with-wifi]').trigger('click')
    expect(value(w, 'ssid')).toBe('IOT')
    await w.get('[data-test=wifi-password]').setValue('short')
    expect(w.get('[data-test=vlan-save]').attributes('disabled')).toBeDefined()
    await w.get('[data-test=wifi-password]').setValue('correct horse')
    await w.get('[data-test=vlan-form]').trigger('submit')
    await flushPromises()
    expect(api.setWifi).toHaveBeenCalledWith({
      ssid: 'IOT',
      previous: undefined,
      vlan: 10,
      password: 'correct horse',
      enabled: true,
    })
  })

  it('renames a Wi-Fi network keeping its password, and removes it', async () => {
    vi.mocked(api.updateVlan).mockResolvedValue({ vlan: lan, changes: [] })
    const withWifi = { ...lan, id: 30, wifi: [{ ssid: 'Guests', enabled: true, nodes: ['ap'] }] }
    let w = form({ vlan: withWifi, wifi: true })
    await w.get('[data-test=ssid]').setValue('Visitors')
    await w.get('[data-test=vlan-form]').trigger('submit')
    await flushPromises()
    expect(api.setWifi).toHaveBeenCalledWith(
      expect.objectContaining({
        ssid: 'Visitors',
        previous: 'Guests',
        vlan: 30,
        password: undefined,
      }),
    )
    w = form({ vlan: withWifi, wifi: true })
    await w.get('[data-test=with-wifi]').trigger('click')
    await w.get('[data-test=vlan-form]').trigger('submit')
    await flushPromises()
    expect(api.deleteWifi).toHaveBeenCalledWith('Guests')
  })

  it('leaves the Wi-Fi alone when nothing in it changed', async () => {
    vi.mocked(api.updateVlan).mockResolvedValue({ vlan: lan, changes: [] })
    const withWifi = { ...lan, id: 30, wifi: [{ ssid: 'Guests', enabled: true, nodes: ['ap'] }] }
    const w = form({ vlan: withWifi, wifi: true })
    await w.get('[data-test=vlan-form]').trigger('submit')
    await flushPromises()
    expect(api.setWifi).not.toHaveBeenCalled()
  })
})
