import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import DeviceExtras from '../map/DeviceExtras.vue'
import { plugins } from './helpers'

describe('DeviceExtras', () => {
  it('shows the state table, DHCP pools, VLANs, VPN peers and services', () => {
    const w = mount(DeviceExtras, {
      props: {
        device: {
          key: 'fw',
          name: 'fw',
          firewall_states: { current: 950, limit: 1000 },
          dhcp_pools: [{ network: 'LAN', total: 100, used: 40 }],
          vlans: [{ id: 20, name: 'IOT', interface: 'igc1.20' }],
          vpn_peers: [
            { name: 'phone', protocol: 'wireguard', connected: true, endpoint: '1.2.3.4:51820' },
          ],
          services: [
            { name: 'unbound', description: 'Unbound DNS', running: true },
            { name: 'ntpd', description: 'Network Time', running: false },
          ],
        },
      },
      global: { plugins: plugins() },
    })
    expect(w.get('[data-test=fw-states] .meter').classes()).toContain('bad') // 95 %
    expect(w.get('[data-test=dhcp-pool]').text()).toContain('40 / 100')
    expect(w.get('[data-test=vlans]').text()).toContain('IOT')
    expect(w.get('[data-test=vpn]').text()).toContain('WireGuard')
    const services = w.get('[data-test=services]')
    expect(services.text()).toContain('1 stopped')
    expect(services.findAll('li')[0]!.text()).toBe('Network Time') // stopped first
  })

  it('shows nothing for a device without these data', () => {
    const w = mount(DeviceExtras, {
      props: { device: { key: 'x', name: 'x' } },
      global: { plugins: plugins() },
    })
    expect(w.text()).toBe('')
  })

  it("shows an access point's radios compactly, with the channels in the summary", () => {
    const w = mount(DeviceExtras, {
      props: {
        device: {
          key: 'ap',
          name: 'ap',
          wireless_uplink: true,
          radios: [
            { name: 'wifi0', band: '2.4ghz', channel: 6, width_mhz: 20, tx_power_dbm: 17 },
            { name: 'wifi1', band: '5ghz', channel: 36, width_mhz: 80, auto: true },
            { name: 'wifi2', band: '6ghz', enabled: false },
          ],
        },
      },
      global: { plugins: plugins() },
    })
    const section = w.get('[data-test=radios]')
    expect(section.get('.summary').text()).toBe('2.4 GHz 6 · 5 GHz 36')
    const rows = section.findAll('[data-test=radio]')
    expect(rows[0]!.text()).toContain('2.4 GHz')
    expect(rows[0]!.text()).toContain('Channel 6 · 20 MHz')
    expect(rows[0]!.text()).toContain('17 dBm · wifi0')
    expect(rows[1]!.text()).toContain('auto')
    expect(rows[2]!.classes()).toContain('off')
    expect(section.text()).toContain('A mesh unit')
  })
})
