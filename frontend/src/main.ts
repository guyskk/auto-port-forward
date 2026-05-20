// M1 占位入口；M6 替换为真正的 Vue + Naive UI 初始化。
import { createApp, h } from 'vue'

createApp({
  render: () => h('div', { style: 'padding:40px;font-family:system-ui;' }, 'autoportforward — skeleton'),
}).mount('#app')
