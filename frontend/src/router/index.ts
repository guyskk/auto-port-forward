// router/index.ts —— hash 路由：/monitor /settings。
//
// 使用 hash 模式以兼容 wails 内嵌前端（无 history server）。
// 服务器管理路由已删除：host 列表直接显示在监控页面。

import { createRouter, createWebHashHistory } from 'vue-router'

export const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/', redirect: '/monitor' },
    { path: '/monitor', name: 'monitor', component: () => import('../views/MonitorView.vue') },
    { path: '/settings', name: 'settings', component: () => import('../views/SettingsView.vue') },
  ],
})
