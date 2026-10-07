import { describe, expect, it } from 'vitest'

import { iconChoice, logoColor, logos } from '../icons'
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
    expect(iconChoice({ type: 'appliance', brand: 'midea' })).toEqual({
      type: 'air-vent',
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
