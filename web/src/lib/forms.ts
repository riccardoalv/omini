import type { Config, FormField } from './types'

/** Initial form values: existing config first, then field defaults. */
export function initialValues(fields: FormField[], existing: Config = {}): Config {
  const out: Config = {}
  for (const f of fields) {
    const value = existing[f.key] ?? f.default
    if (value !== undefined) out[f.key] = value
    else if (f.type === 'bool') out[f.key] = false
    else out[f.key] = ''
  }
  return out
}

/** Converts form values to the types the API expects (numbers for int fields). */
export function toConfig(fields: FormField[], values: Config): Config {
  const out: Config = {}
  for (const f of fields) {
    const v = values[f.key]
    if (v === '' || v === undefined) continue
    out[f.key] = f.type === 'int' ? Number(v) : v
  }
  return out
}

/** Keys of required fields left empty. */
export function missingRequired(fields: FormField[], values: Config): string[] {
  return fields
    .filter((f) => f.required && (values[f.key] === '' || values[f.key] === undefined))
    .map((f) => f.key)
}

/** HTML input type for a field. */
export function inputType(field: FormField): string {
  switch (field.type) {
    case 'secret':
      return 'password'
    case 'int':
      return 'number'
    case 'url':
      return 'url'
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
