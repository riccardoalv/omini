import { createAppI18n } from '@/i18n'

/** Plugins needed to mount components in tests (English UI). */
export const plugins = () => [createAppI18n('en')]
