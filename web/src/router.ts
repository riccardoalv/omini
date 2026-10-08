import { createRouter, createWebHistory } from 'vue-router'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'map', component: () => import('./views/MapView.vue') },
    { path: '/devices', name: 'devices', component: () => import('./views/DevicesView.vue') },
    {
      path: '/integrations',
      name: 'integrations',
      component: () => import('./views/IntegrationsView.vue'),
    },
    { path: '/store', name: 'store', component: () => import('./views/StoreView.vue') },
    { path: '/insights', name: 'insights', component: () => import('./views/InsightsView.vue') },
    { path: '/settings', name: 'settings', component: () => import('./views/SettingsView.vue') },
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
})
