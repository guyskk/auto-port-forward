<script setup lang="ts">
import { computed, h } from 'vue'
import { NDataTable, NSpace, NButton, NText } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import type { Forward } from '../types'
import StatusTag from './StatusTag.vue'
import { zh } from '../i18n/zh'

const props = defineProps<{ data: ReadonlyArray<Forward> }>()
const emit = defineEmits<{ toggle: [serverId: string, port: number, on: boolean] }>()

function fmtBind(f: Forward): string {
  return `${f.remote.bind_addr}:${f.remote_port}`
}

const columns = computed<DataTableColumns<Forward>>(() => [
  {
    title: zh.monitor.columns.remote_port,
    key: 'remote_port',
    sorter: 'default',
    width: 130,
    render(row) {
      return h(
        NSpace,
        { size: 4, vertical: true },
        {
          default: () => [
            h('strong', null, String(row.remote_port)),
            h(NText, { depth: 3, style: 'font-size:11px' }, () => fmtBind(row)),
          ],
        },
      )
    },
  },
  {
    title: zh.monitor.columns.process,
    key: 'remote.process',
    render(row) {
      return h(NSpace, { size: 4, vertical: true }, {
        default: () => [
          h('span', null, row.remote.process || '—'),
          row.remote.pid > 0
            ? h(NText, { depth: 3, style: 'font-size:11px' }, () => `pid ${row.remote.pid}`)
            : null,
        ],
      })
    },
  },
  {
    title: zh.monitor.columns.docker,
    key: 'remote.docker_image',
    width: 160,
    render(row) {
      return row.remote.docker_image
        ? h(NText, null, () => row.remote.docker_image)
        : h(NText, { depth: 3 }, () => '—')
    },
  },
  {
    title: zh.monitor.columns.status,
    key: 'status',
    width: 110,
    render(row) {
      return h(StatusTag, { status: row.status })
    },
  },
  {
    title: zh.monitor.columns.local_port,
    key: 'local_port',
    width: 120,
    render(row) {
      return h('span', null, String(row.local_port))
    },
  },
  {
    title: zh.monitor.columns.note,
    key: 'error',
    render(row) {
      if (row.error) {
        return h(NText, { type: 'error', style: 'font-size:12px' }, () => row.error)
      }
      return h(NText, { depth: 3 }, () => '')
    },
  },
  {
    title: zh.monitor.columns.action,
    key: 'action',
    width: 100,
    render(row) {
      const on = row.status === 'forwarding' || row.status === 'pending'
      return h(
        NButton,
        {
          size: 'tiny',
          quaternary: true,
          onClick: () => emit('toggle', row.server_id, row.remote_port, !on),
        },
        { default: () => (on ? '禁用' : '启用') },
      )
    },
  },
])

const rowKey = (row: Forward) => `${row.server_id}/${row.remote_port}`

// NDataTable 的 :data 类型是 RowData[]（mutable）；上游传 ReadonlyArray<Forward>
// 是为了让 selector 输出引用稳定。这里只 cast 类型，运行时仍是同一引用。
const tableData = computed(() => props.data as Forward[])
</script>

<template>
  <n-data-table
    :data="tableData"
    :columns="columns"
    :row-key="rowKey"
    size="small"
    :pagination="false"
    :bordered="false"
  />
</template>
