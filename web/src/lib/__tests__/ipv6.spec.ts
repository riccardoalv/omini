import { describe, expect, it } from 'vitest'

import { compactIPv6, globalIPv6, isLinkLocal6 } from '../ipv6'

describe('ipv6', () => {
  it('writes addresses compact', () => {
    expect(compactIPv6('2001:0DB8:0000:0000:0000:0000:0000:0020')).toBe('2001:db8::20')
    expect(compactIPv6('2001:db8:0:0:1:0:0:1')).toBe('2001:db8::1:0:0:1')
    expect(compactIPv6('2001:db8:0:1:1:1:1:1')).toBe('2001:db8:0:1:1:1:1:1')
    expect(compactIPv6('fe80:0:0:0::1%eth0')).toBe('fe80::1%eth0')
    expect(compactIPv6('::')).toBe('::')
    expect(compactIPv6('0:0:0:0:0:0:0:1')).toBe('::1')
  })

  it('leaves anything else as it is', () => {
    for (const s of ['192.168.1.10', 'nas.home.lan', '::ffff:1.2.3.4', '1::2::3', '2001:db8::zz'])
      expect(compactIPv6(s)).toBe(s)
  })

  it('tells link-local addresses apart', () => {
    expect(isLinkLocal6('fe80::1')).toBe(true)
    expect(isLinkLocal6('febf::1')).toBe(true)
    expect(isLinkLocal6('fec0::1')).toBe(false)
    expect(globalIPv6(['2001:db8::1', 'fe80::1', 'fd00::5'])).toEqual(['2001:db8::1', 'fd00::5'])
    expect(globalIPv6(undefined)).toEqual([])
  })
})
