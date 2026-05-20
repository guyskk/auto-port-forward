<script setup lang="ts">
import { NTag } from 'naive-ui'
import { computed } from 'vue'
import type { PortStatus } from '../types'
import { zh } from '../i18n/zh'

const props = defineProps<{ status: PortStatus }>()

const typeMap: Record<PortStatus, 'success' | 'default' | 'warning' | 'error'> = {
  forwarding: 'success',
  pending: 'default',
  excluded: 'default',
  conflict: 'error',
  conflict_priv: 'error',
  error: 'error',
}

const label = computed(() => zh.status[props.status] ?? props.status)
const type = computed(() => typeMap[props.status] ?? 'default')
</script>

<template>
  <n-tag :type="type" size="small" round>{{ label }}</n-tag>
</template>
