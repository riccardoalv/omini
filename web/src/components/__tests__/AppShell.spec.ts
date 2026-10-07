import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import { prefs } from '@/lib/prefs'

import AppShell from '../AppShell.vue'
import { plugins } from './helpers'

const router = createRouter({
  history: createMemoryHistory(),
  routes: ['/', '/devices', '/integrations', '/settings'].map((path) => ({
    path,
    component: { template: '<div />' },
  })),
})

describe('AppShell', () => {
  beforeEach(() => {
    prefs.sidebarExpanded = false
  })

  it('expands and collapses the sidebar, remembering the choice', async () => {
    const w = mount(AppShell, { global: { plugins: [...plugins(), router] } })
    const nav = w.get('nav')
    const toggle = w.get('[data-test=sidebar-toggle]')
    expect(nav.classes()).not.toContain('expanded')
    expect(toggle.attributes('aria-expanded')).toBe('false')

    await toggle.trigger('click')
    expect(nav.classes()).toContain('expanded')
    expect(prefs.sidebarExpanded).toBe(true)
    expect(toggle.text()).toBe('Collapse menu')

    await toggle.trigger('click')
    expect(nav.classes()).not.toContain('expanded')
  })

  it('links to every screen', () => {
    const w = mount(AppShell, { global: { plugins: [...plugins(), router] } })
    expect(w.findAll('a').map((a) => a.attributes('href'))).toEqual([
      '/',
      '/devices',
      '/integrations',
      '/settings',
    ])
  })
})
