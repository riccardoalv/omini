import { describe, expect, it } from 'vitest'

import { fieldLabel, initialValues, inputType, missingRequired, toConfig } from '../forms'
import type { FormField } from '../types'

const fields: FormField[] = [
  { key: 'host', type: 'host', label: 'Host', required: true },
  { key: 'community', type: 'secret', required: true, default: 'public' },
  { key: 'port', type: 'int', default: 161 },
  { key: 'verify_tls', type: 'bool' },
]

describe('forms', () => {
  it('starts from existing values, then defaults', () => {
    expect(initialValues(fields)).toEqual({
      host: '',
      community: 'public',
      port: 161,
      verify_tls: false,
    })
    expect(initialValues(fields, { host: '10.0.0.2', port: 1161 })).toMatchObject({
      host: '10.0.0.2',
      port: 1161,
    })
  })

  it('converts values for the API: numbers for int fields, empty values dropped', () => {
    expect(
      toConfig(fields, { host: '10.0.0.2', community: '', port: '1161', verify_tls: true }),
    ).toEqual({
      host: '10.0.0.2',
      port: 1161,
      verify_tls: true,
    })
  })

  it('lists missing required fields', () => {
    expect(missingRequired(fields, initialValues(fields))).toEqual(['host'])
  })

  it('maps field types to inputs and labels', () => {
    expect(inputType(fields[1]!)).toBe('password')
    expect(inputType(fields[2]!)).toBe('number')
    expect(fieldLabel(fields[0]!)).toBe('Host')
    expect(fieldLabel(fields[3]!)).toBe('Verify tls')
  })
})
