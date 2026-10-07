import { describe, expect, it } from 'vitest'

import { formatAgo, formatBytes, formatSpeed, formatUptime } from '../format'

describe('format', () => {
  it('formats link speeds', () => {
    expect(formatSpeed(undefined)).toBe('')
    expect(formatSpeed(100)).toBe('100M')
    expect(formatSpeed(1000)).toBe('1G')
    expect(formatSpeed(2500)).toBe('2.5G')
    expect(formatSpeed(10000)).toBe('10G')
  })

  it('formats bytes', () => {
    expect(formatBytes(512)).toBe('512 B')
    expect(formatBytes(1536)).toBe('1.5 KiB')
    expect(formatBytes(5 * 1024 ** 3)).toBe('5.0 GiB')
  })

  it('formats uptime', () => {
    expect(formatUptime(45 * 60)).toBe('45m')
    expect(formatUptime(3 * 3600 + 12 * 60)).toBe('3h 12m')
    expect(formatUptime(41 * 86400 + 7 * 3600)).toBe('41d 7h')
  })

  it('formats relative time in the user locale', () => {
    const now = Date.parse('2026-10-06T12:00:00Z')
    expect(formatAgo('2026-10-06T11:55:00Z', 'en', now)).toBe('5 minutes ago')
    expect(formatAgo('2026-10-06T11:55:00Z', 'pt-BR', now)).toBe('há 5 minutos')
    expect(formatAgo('2026-10-04T12:00:00Z', 'en', now)).toBe('2 days ago')
  })
})
