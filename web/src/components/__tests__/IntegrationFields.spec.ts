import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import { createAppI18n } from '@/i18n'
import type { FormField } from '@/lib/types'

import IntegrationFields from '../IntegrationFields.vue'

const fields: FormField[] = [
  {
    key: 'device_versions',
    type: 'select',
    label: 'Service versions',
    options: ['off', 'quick', 'light', 'full'],
    group: 'Scan one device',
  },
  { key: 'mode', type: 'select', label: 'Mode', options: ['fast', 'slow'] },
]

const mountFields = (locale: 'en' | 'pt-BR') =>
  mount(IntegrationFields, {
    props: { type: 'nmap', fields, modelValue: { device_versions: 'light', mode: 'fast' } },
    global: { plugins: [createAppI18n(locale)] },
  })

describe('IntegrationFields select options', () => {
  it('translates option labels and keeps the values', () => {
    const w = mountFields('pt-BR')
    const opts = w.findAll('select[name=device_versions] option')
    expect(opts.map((o) => o.text())).toEqual(['Desligado', 'Rápida', 'Leve', 'Completa'])
    expect(opts.map((o) => o.attributes('value'))).toEqual(['off', 'quick', 'light', 'full'])
    expect(w.get('legend').text()).toBe('Escanear um dispositivo')
  })

  it('shows the value itself when there is no translation', () => {
    const w = mountFields('en')
    expect(w.findAll('select[name=device_versions] option').map((o) => o.text())).toEqual([
      'Off',
      'Quick',
      'Light',
      'Full',
    ])
    expect(w.findAll('select[name=mode] option').map((o) => o.text())).toEqual(['fast', 'slow'])
  })
})
