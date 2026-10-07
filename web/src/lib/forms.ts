import type { Config, FormField } from './types'

/** Value the API returns instead of a saved secret; sending it back keeps the secret. */
export const MASKED = '********'

/**
 * Initial form values: existing config first, then field defaults. Saved
 * secrets come back masked from the API, so their fields start empty
 * ("leave blank to keep").
 */
export function initialValues(fields: FormField[], existing: Config = {}): Config {
  const out: Config = {}
  for (const f of fields) {
    if (f.type === 'secret' && existing[f.key] === MASKED) {
      out[f.key] = ''
      continue
    }
    const value = existing[f.key] ?? f.default
    if (value !== undefined) out[f.key] = value
    else if (f.type === 'bool') out[f.key] = false
    else out[f.key] = ''
  }
  return out
}

/**
 * Converts form values to the types the API expects (numbers for int fields).
 * With keepSecrets (editing), empty secret fields keep the saved value.
 */
export function toConfig(fields: FormField[], values: Config, keepSecrets = false): Config {
  const out: Config = {}
  for (const f of fields) {
    const v = values[f.key]
    if (keepSecrets && f.type === 'secret' && (v === '' || v === undefined)) {
      out[f.key] = MASKED
      continue
    }
    if (v === '' || v === undefined) continue
    out[f.key] = f.type === 'int' ? Number(v) : v
  }
  return out
}

/** Keys of required fields left empty (saved secrets count as filled when keepSecrets). */
export function missingRequired(
  fields: FormField[],
  values: Config,
  keepSecrets = false,
): string[] {
  return fields
    .filter((f) => f.required && !(keepSecrets && f.type === 'secret'))
    .filter((f) => values[f.key] === '' || values[f.key] === undefined)
    .map((f) => f.key)
}

/** HTML input type for a field. */
export function inputType(field: FormField): string {
  switch (field.type) {
    case 'secret':
      return 'password'
    case 'int':
      return 'number'
    // Addresses are text: "192.168.1.1" is fine (the server assumes https://),
    // while the browser's url type would demand the scheme.
    default:
      return 'text'
  }
}

/** Human label for a field when the integration does not provide one. */
export function fieldLabel(field: FormField): string {
  if (field.label) return field.label
  const words = field.key.replace(/_/g, ' ')
  return words.charAt(0).toUpperCase() + words.slice(1)
}
