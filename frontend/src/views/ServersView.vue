<script setup lang="ts">
import { computed, h, ref } from 'vue'
import {
  NSpace,
  NButton,
  NDataTable,
  NModal,
  NTag,
  NPopconfirm,
  useMessage,
} from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import ServerForm from '../components/ServerForm.vue'
import { useAppStore } from '../store/state'
import type { Server } from '../types'
import { zh } from '../i18n/zh'

const store = useAppStore()
const message = useMessage()

const showForm = ref(false)
const editing = ref<Partial<Server> | undefined>(undefined)

function openCreate() {
  editing.value = undefined
  showForm.value = true
}
function openEdit(row: Server) {
  editing.value = { ...row }
  showForm.value = true
}
function closeForm() {
  showForm.value = false
}

async function onSubmit(s: Server) {
  if (s.id) {
    await store.updateServer(s)
    message.success('已保存')
  } else {
    await store.addServer(s)
    message.success('已新增')
  }
  showForm.value = false
}

async function onDelete(id: string) {
  await store.deleteServer(id)
  message.info('已删除')
}

async function onTest(id: string) {
  try {
    await window.go?.main?.App?.TestServer?.(id)
    message.success(zh.servers.testOK)
  } catch (e) {
    message.error(`${zh.servers.testFail}: ${e instanceof Error ? e.message : String(e)}`)
  }
}

const columns = computed<DataTableColumns<Server>>(() => [
  { title: zh.servers.columns.name, key: 'name', render: (r) => r.name || r.id },
  { title: zh.servers.columns.host, key: 'host', render: (r) => `${r.host}:${r.port}` },
  { title: zh.servers.columns.user, key: 'user' },
  {
    title: zh.servers.columns.auth,
    key: 'auth_method',
    render: (r) => h(NTag, { size: 'small' }, () => r.auth_method),
  },
  {
    title: zh.servers.columns.enabled,
    key: 'enabled',
    render: (r) =>
      h(NTag, { type: r.enabled ? 'success' : 'default', size: 'small' }, () =>
        r.enabled ? 'ON' : 'OFF',
      ),
  },
  {
    title: zh.servers.columns.action,
    key: 'action',
    render(row) {
      return h(NSpace, { size: 4 }, () => [
        h(NButton, { size: 'tiny', onClick: () => openEdit(row) }, () => zh.servers.edit),
        h(NButton, { size: 'tiny', onClick: () => onTest(row.id) }, () => zh.servers.test),
        h(
          NPopconfirm,
          { onPositiveClick: () => onDelete(row.id) },
          {
            trigger: () =>
              h(NButton, { size: 'tiny', type: 'error', quaternary: true }, () => zh.servers.delete),
            default: () => zh.servers.confirmDelete,
          },
        ),
      ])
    },
  },
])
</script>

<template>
  <n-space vertical :size="16">
    <n-space justify="space-between">
      <n-button type="primary" @click="openCreate">{{ zh.servers.add }}</n-button>
    </n-space>
    <n-data-table
      :data="store.servers"
      :columns="columns"
      :row-key="(r: Server) => r.id"
      size="small"
      :pagination="false"
    />
    <n-modal
      v-model:show="showForm"
      :title="editing ? zh.servers.edit : zh.servers.add"
      preset="card"
      style="width: 560px"
      :mask-closable="false"
    >
      <server-form :initial="editing" @submit="onSubmit" @cancel="closeForm" />
    </n-modal>
  </n-space>
</template>
