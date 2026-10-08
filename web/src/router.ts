import { createRouter, createWebHistory } from 'vue-router'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'map', component: () => import('./views/MapView.vue') },
    { path: '/devices', redirect: { path: '/', query: { devices: '1' } } }, // the list is a drawer on the map
    {
      path: '/integrations',
      name: 'integrations',
      component: () => import('./views/IntegrationsView.vue'),
    },
    { path: '/flows', name: 'flows', component: () => import('./views/FlowsView.vue') },
    { path: '/store', redirect: { path: '/integrations', query: { store: '1' } } }, // the store is a modal there
    { path: '/alerts', name: 'alerts', component: () => import('./views/AlertsView.vue') },
    { path: '/insights', redirect: (to) => ({ path: '/alerts', query: to.query }) }, // old address
    { path: '/settings', name: 'settings', component: () => import('./views/SettingsView.vue') },
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
})
