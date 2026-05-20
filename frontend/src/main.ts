// main.ts —— Vue 应用入口。
//
// 注册 Naive UI、Pinia、Router；挂载 #app。
// pinia store 的 subscribe() 在 App.vue onMounted 调用，确保 wails runtime 已就绪。

import { createApp } from 'vue'
import { createPinia } from 'pinia'
import naive from 'naive-ui'
import App from './App.vue'
import { router } from './router'
import './style.css'

const app = createApp(App)
app.use(createPinia())
app.use(router)
app.use(naive)
app.mount('#app')
