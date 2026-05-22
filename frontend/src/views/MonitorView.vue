<script setup lang="ts">
import { computed, onMounted } from 'vue'
import {
  NSpace,
  NButton,
  NEmpty,
  NText,
  NTag,
  NSwitch,
  NCollapse,
  NCollapseItem,
  NAlert,
  useMessage,
} from 'naive-ui'
import PortTable from '../components/PortTable.vue'
import { useAppStore } from '../store/state'
import { zh } from '../i18n/zh'
import type { ServerConnState } from '../types'
import type { HostView } from '../core/selectors'

const store = useAppStore()
const message = useMessage()

const lastScanLabel = computed(() => {
  if (!store.lastScanAt) return zh.monitor.never
  return new Date(store.lastScanAt).toLocaleTimeString('zh-CN')
})

const STATE_LABELS: Record<
  ServerConnState,
  { label: string; type: 'default' | 'success' | 'warning' | 'error' }
> = {
  dialing: { label: zh.conn.dialing, type: 'warning' },
  connected: { label: zh.conn.connected, type: 'success' },
  broken: { label: zh.conn.broken, type: 'error' },
  degraded: { label: zh.conn.degraded, type: 'error' },
}

function connLabel(h: HostView): { label: string; type: 'default' | 'success' | 'warning' | 'error' } {
  if (!h.status) return { label: zh.conn.unknown, type: 'default' }
  return STATE_LABELS[h.status.state] ?? { label: zh.conn.unknown, type: 'default' }
}

function connError(h: HostView): string | null {
  if (!h.status || !h.status.error) return null
  if (h.status.state === 'connected') return null
  return h.status.error
}

async function onScan() {
  try {
    await store.scanNow()
  } catch (e) {
    message.error(String((e as Error)?.message ?? e))
  }
}

async function onReload() {
  try {
    await store.reloadSSHConfig()
    message.success('已刷新')
  } catch (e) {
    message.error(String((e as Error)?.message ?? e))
  }
}

async function onToggleHost(alias: string, on: boolean) {
  try {
    await store.setHostEnabled(alias, on)
  } catch (e) {
    message.error(String((e as Error)?.message ?? e))
  }
}

async function onTestHost(alias: string) {
  try {
    await store.testHost(alias)
    message.success(zh.hosts.testOK)
  } catch (e) {
    message.error(`${zh.hosts.testFail}: ${(e as Error)?.message ?? e}`)
  }
}

async function onTogglePort(serverId: string, port: number, on: boolean) {
  // 关键性能优化：仅触发后端意图，等待事件回送真实状态。
  // 移除了原先的 `await store.scanNow()`——避免「一次点击 → 一次 RPC + 一次额外快照拉取
  // + 一次全量模板重渲染」的雪崩链路。
  try {
    await store.toggleForward(serverId, port, on)
  } catch (e) {
    message.error(String((e as Error)?.message ?? e))
  }
}

// 卡片显示顺序：enabled 优先，再按 alias 字典序。
// 注意：store.hostsView 已经是 memoized HostView[]——只要底层
// hosts/enabledHosts/serverStatus 三个 ref 未变，computed 不重算。
const sortedHosts = computed<HostView[]>(() => {
  const list = store.hostsView.slice()
  list.sort((a, b) => {
    const ea = a.enabled ? 0 : 1
    const eb = b.enabled ? 0 : 1
    if (ea !== eb) return ea - eb
    return a.alias.localeCompare(b.alias)
  })
  return list
})

onMounted(async () => {
  if (!store.config) {
    await store.refresh()
  }
})
</script>

<template>
  <n-space vertical :size="16">
    <n-space justify="space-between" align="center">
      <n-space>
        <n-button type="primary" @click="onScan" :loading="store.loading">
          {{ zh.monitor.scanNow }}
        </n-button>
        <n-button @click="onReload">{{ zh.monitor.reload }}</n-button>
      </n-space>
      <n-text depth="3">{{ zh.monitor.lastScan }}: {{ lastScanLabel }}</n-text>
    </n-space>

    <n-empty
      v-if="sortedHosts.length === 0"
      :description="zh.monitor.emptyHosts"
    />

    <n-collapse
      v-else
      :default-expanded-names="sortedHosts.filter((h) => h.enabled).map((h) => h.alias)"
    >
      <n-collapse-item
        v-for="h in sortedHosts"
        :key="h.alias"
        :name="h.alias"
        :title="h.alias"
      >
        <template #header-extra>
          <n-space :size="8" align="center" @click.stop>
            <n-text depth="3" style="font-size: 12px">
              {{ h.user }}@{{ h.host_name }}:{{ h.port }}
            </n-text>
            <n-tag :type="connLabel(h).type" size="small" round>
              {{ connLabel(h).label }}
            </n-tag>
            <n-text style="font-size: 12px">{{ zh.hosts.monitor }}</n-text>
            <n-switch
              :value="h.enabled"
              size="small"
              @update:value="(v: boolean) => onToggleHost(h.alias, v)"
            />
            <n-button size="tiny" @click.stop="onTestHost(h.alias)">
              {{ zh.hosts.test }}
            </n-button>
          </n-space>
        </template>

        <n-space vertical :size="8">
          <n-alert
            v-if="connError(h)"
            type="warning"
            :show-icon="false"
            style="font-size: 12px"
          >
            {{ connError(h) }}
            <br />
            <n-text depth="3" style="font-size: 11px">
              {{ zh.monitor.hostKeyTip.replace('{alias}', h.alias) }}
            </n-text>
          </n-alert>

          <template v-if="h.enabled">
            <port-table
              v-if="store.forwardsByAlias(h.alias).length > 0"
              :data="store.forwardsByAlias(h.alias)"
              @toggle="onTogglePort"
            />
            <n-empty
              v-else
              :description="zh.monitor.emptyPort"
              style="padding: 12px 0"
            />
          </template>
          <n-text v-else depth="3" style="font-size: 12px">
            {{ zh.monitor.offNote }}
          </n-text>
        </n-space>
      </n-collapse-item>
    </n-collapse>
  </n-space>
</template>
