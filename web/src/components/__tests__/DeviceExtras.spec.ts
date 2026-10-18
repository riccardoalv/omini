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

  it('lists what a router exposes to the internet, read-only, UPnP ones first', () => {
    const w = mount(DeviceExtras, {
      props: {
        device: {
          key: 'gw',
          name: 'gw',
          port_forwards: [
            {
              protocol: 'tcp',
              external_port: 443,
              internal_ip: '192.168.1.10',
              description: 'Nextcloud',
              source: '203.0.113.0/24',
            },
            {
              protocol: 'udp',
              external_port: 27015,
              external_port_end: 27020,
              internal_ip: '192.168.1.50',
              upnp: true,
            },
            { protocol: 'tcp/udp', external_port: 8080, internal_ip: 'fd00::5', enabled: false },
          ],
        },
        nameAt: (ip: string) => (ip === '192.168.1.10' ? 'nas' : undefined),
      },
      global: { plugins: plugins() },
    })
    const section = w.get('[data-test=exposed]')
    expect(section.get('.summary').text()).toBe('2 · 1 by UPnP')
    const rows = section.findAll('[data-test=forward]')
    expect(rows.map((r) => r.get('.port').text())).toEqual(['27015-27020', '443', '8080'])
    expect(rows[0]!.text()).toContain('→ 192.168.1.50:27015-27020')
    expect(rows[0]!.text()).toContain('UPnP')
    expect(rows[0]!.text()).toContain('from anyone')
    expect(rows[1]!.text()).toContain('→ nas:443')
    expect(rows[1]!.text()).toContain('TCP')
    expect(rows[1]!.text()).toContain('Nextcloud')
    expect(rows[1]!.text()).toContain('from 203.0.113.0/24')
    expect(rows[2]!.text()).toContain('[fd00::5]:8080')
    expect(rows[2]!.classes()).toContain('off')
    expect(section.find('button:not([data-test^=fold])').exists()).toBe(false)
  })
})
