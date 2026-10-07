import { createI18n } from 'vue-i18n'

import en from './en'
import ptBR from './pt-BR'

export const locales = [
  { code: 'en', name: 'English' },
  { code: 'pt-BR', name: 'Português (Brasil)' },
] as const

export function createAppI18n(locale: string) {
  return createI18n({
    legacy: false,
    locale: locales.some((l) => l.code === locale) ? locale : 'en',
    fallbackLocale: 'en',
    messages: { en, 'pt-BR': ptBR },
  })
}
