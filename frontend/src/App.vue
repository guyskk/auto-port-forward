<script setup lang="ts">
import { onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  NConfigProvider,
  NLayout,
  NLayoutHeader,
  NLayoutSider,
  NLayoutContent,
  NMenu,
  NSpace,
  NText,
  NTag,
  NMessageProvider,
  NDialogProvider,
  dateZhCN,
  zhCN,
} from 'naive-ui'
import { computed } from 'vue'
import { useAppStore } from './store/state'
import { isWails } from './api/wails'
import { zh } from './i18n/zh'

const store = useAppStore()
const route = useRoute()
const router = useRouter()

const menuOptions = [
  { label: zh.menu.monitor, key: 'monitor' },
  { label: zh.menu.servers, key: 'servers' },
  { label: zh.menu.settings, key: 'settings' },
]

const activeKey = computed(() => (route.name as string) ?? 'monitor')

function onMenu(key: string) {
  router.push({ name: key })
}

onMounted(async () => {
  store.subscribe()
  await store.refresh()
})

const modeTag = computed(() => (isWails() ? '原生' : 'Mock'))
</script>

<template>
  <n-config-provider :locale="zhCN" :date-locale="dateZhCN">
    <n-message-provider>
      <n-dialog-provider>
        <n-layout style="height: 100vh" has-sider>
          <n-layout-sider
            bordered
            collapse-mode="width"
            :collapsed-width="64"
            :width="200"
            :native-scrollbar="false"
          >
            <div style="padding: 16px 18px; font-weight: 600; font-size: 16px">
              {{ zh.appTitle }}
            </div>
            <n-menu
              :value="activeKey"
              :options="menuOptions"
              :on-update:value="onMenu"
            />
          </n-layout-sider>
          <n-layout>
            <n-layout-header bordered style="padding: 10px 18px">
              <n-space justify="space-between" align="center">
                <n-text strong>autoportforward</n-text>
                <n-space :size="8">
                  <n-tag :type="isWails() ? 'success' : 'warning'" size="small">{{ modeTag }}</n-tag>
                  <n-text depth="3" v-if="store.lastError">⚠️ {{ store.lastError }}</n-text>
                </n-space>
              </n-space>
            </n-layout-header>
            <n-layout-content :native-scrollbar="false" style="padding: 16px">
              <router-view />
            </n-layout-content>
          </n-layout>
        </n-layout>
      </n-dialog-provider>
    </n-message-provider>
  </n-config-provider>
</template>
