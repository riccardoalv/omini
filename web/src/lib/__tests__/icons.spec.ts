import { describe, expect, it } from 'vitest'

import { catalog } from '../catalog'
import { iconChoice, logoColor, logos } from '../icons'
import { displayName } from '../names'
import { parseReasons } from '../reasons'

describe('iconChoice', () => {
  it('shows homelab products with their logo only', () => {
    expect(iconChoice({ type: 'nas', product: 'truenas', brand: 'proxmox' })).toEqual({
      logo: 'truenas',
      badge: false,
    })
    expect(iconChoice({ type: 'hypervisor', product: 'proxmox' })).toEqual({
      logo: 'proxmox',
      badge: false,
    })
  })

  it('shows the device type with the OS as a badge', () => {
    expect(iconChoice({ type: 'phone', os: 'android', brand: 'huawei' })).toEqual({
      type: 'smartphone',
      logo: 'android',
      badge: true,
    })
    expect(iconChoice({ type: 'phone', os: 'ios', brand: 'apple' }).logo).toBe('ios')
    expect(iconChoice({ type: 'computer', os: 'nixos' })).toEqual({
      type: 'monitor',
      logo: 'nixos',
      badge: true,
    })
  })

  it('falls back to the brand badge, then to the type alone', () => {
    expect(iconChoice({ type: 'smart_home', brand: 'espressif' })).toEqual({
      type: 'house',
      logo: 'espressif',
      badge: true,
    })
    // Brands without a logo get no badge.
    expect(iconChoice({ type: 'air_conditioner', brand: 'midea' })).toEqual({
      type: 'air-vent',
      logo: undefined,
      badge: false,
    })
    // A Huawei SUN2000: solar panel with the Huawei badge.
    expect(iconChoice({ type: 'solar_inverter', brand: 'huawei' })).toMatchObject({
      type: 'solar-panel',
    })
    expect(iconChoice({ type: 'appliance' })).toEqual({
      type: 'refrigerator',
      logo: undefined,
      badge: false,
    })
    expect(iconChoice({})).toEqual({ type: 'circle-question-mark', logo: undefined, badge: false })
  })

  it('uses the icon chosen by the user', () => {
    expect(iconChoice({ type: 'server', icon: 'jellyfin' })).toEqual({
      logo: 'jellyfin',
      badge: false,
    })
    expect(iconChoice({ type: 'phone', os: 'android', icon: 'samsung' })).toEqual({
      type: 'smartphone',
      logo: 'samsung',
      badge: true,
    })
  })

  it('has a logo for every OS the classifier produces', () => {
    for (const os of [
      'nixos',
      'debian',
      'ubuntu',
      'freebsd',
      'android',
      'ios',
      'macos',
      'windows',
      'tvos',
    ]) {
      expect(logos[os]).toBeDefined()
    }
  })
})

describe('logoColor', () => {
  it('keeps brand colors unless they disappear on the background', () => {
    expect(logoColor('5277C3', true)).toBe('#5277C3') // NixOS blue on dark
    expect(logoColor('000000', true)).toBeUndefined() // Apple black on dark
    expect(logoColor('000000', false)).toBe('#000000')
    expect(logoColor('FFFFFF', false)).toBeUndefined()
  })
})

describe('parseReasons', () => {
  it('turns classifier evidence into labels', () => {
    expect(
      parseReasons(['title:TrueNAS - nas', 'port:8006', 'private-mac', 'user', 'weird']),
    ).toEqual([
      { key: 'reasonTitle', value: 'TrueNAS - nas' },
      { key: 'reasonPort', value: '8006' },
      { key: 'reasonPrivate' },
      { key: 'reasonUser' },
    ])
  })
})

describe('displayName', () => {
  const t = (key: string, p?: Record<string, unknown>) =>
    key === 'names.typeBrand'
      ? `${p!.brand} ${p!.type}`
      : ((
          { 'types.air_conditioner': 'Air conditioner', 'types.phone': 'Phone' } as Record<
            string,
            string
          >
        )[key] ?? key)

  it('keeps real names', () => {
    expect(displayName({ label: 'TRUENAS', ip: '192.168.1.51', product: 'truenas' }, t)).toBe(
      'TRUENAS',
    )
  })

  it('names devices without a name by what they are', () => {
    expect(
      displayName({ label: '192.168.1.54', ip: '192.168.1.54', product: 'homeassistant' }, t),
    ).toBe('Home Assistant')
    expect(
      displayName(
        { label: '192.168.1.240', ip: '192.168.1.240', type: 'air_conditioner', brand: 'midea' },
        t,
      ),
    ).toBe('Midea Air conditioner')
    expect(displayName({ label: '192.168.1.41', ip: '192.168.1.41', type: 'phone' }, t)).toBe(
      'Phone',
    )
  })

  it('falls back to the address when nothing is known', () => {
    expect(displayName({ label: '192.168.1.9', ip: '192.168.1.9', type: 'unknown' }, t)).toBe(
      '192.168.1.9',
    )
  })
})

describe('app catalog icons', () => {
  it('uses catalog icons for apps and brands without a Simple Icons logo', () => {
    catalog.names = new Set(['mercusys', 'qbittorrent'])
    expect(iconChoice({ kind: 'app', type: 'app', product: 'qbittorrent' })).toEqual({
      type: 'app-window',
      remote: 'qbittorrent',
      badge: false,
    })
    expect(iconChoice({ type: 'ap', brand: 'mercusys' })).toEqual({
      type: 'wifi',
      remote: 'mercusys',
      badge: true,
    })
    // Not in the catalog either: no badge.
    expect(iconChoice({ type: 'air_conditioner', brand: 'midea' }).badge).toBe(false)
    catalog.names = new Set()
  })
})
