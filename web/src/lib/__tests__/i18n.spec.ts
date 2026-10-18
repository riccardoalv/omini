import { describe, expect, it } from 'vitest'

import ptBR from '@/i18n/pt-BR'

/**
 * The built-in integrations' form fields (internal/netscan, internal/flows):
 * their English labels come from the server, so pt-BR must have each one or
 * the form falls back to English.
 */
const builtin: Record<string, string[]> = {
  network: [
    'subnets',
    'learned_subnets',
    'arp',
    'ping',
    'port_scan',
    'dns',
    'netbios',
    'mdns',
    'ssdp',
    'web_titles',
    'ssh_banners',
    'dhcp',
    'dhcp_check',
    'dns_check',
    'snmp',
    'snmp_communities',
    'snmp_write_community',
    'snmp_v3_user',
    'snmp_v3_auth',
    'snmp_v3_auth_pass',
    'snmp_v3_priv',
    'snmp_v3_priv_pass',
    'ports',
    'dns_test_name',
    'deep_interval',
  ],
  flows: ['netflow_port', 'sflow_port'],
}

describe('pt-BR', () => {
  it("labels every field of the built-in integrations' forms", () => {
    const missing = Object.entries(builtin).flatMap(([type, keys]) =>
      keys.filter((k) => !ptBR.fields[type]?.[k]?.label).map((k) => `${type}.${k}`),
    )
    expect(missing).toEqual([])
  })
})
