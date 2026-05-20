// router/index.ts —— hash 路由：/monitor /servers /settings。
//
// 使用 hash 模式以兼容 wails 内嵌前端（无 history server）。

import { createRouter, createWebHashHistory } from 'vue-router'

export const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/', redirect: '/monitor' },
    { path: '/monitor', name: 'monitor', component: () => import('../views/MonitorView.vue') },
    { path: '/servers', name: 'servers', component: () => import('../views/ServersView.vue') },
    { path: '/settings', name: 'settings', component: () => import('../views/SettingsView.vue') },
  ],
})
