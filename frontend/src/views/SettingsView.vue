<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import {
  NSpace,
  NCard,
  NForm,
  NFormItem,
  NInputNumber,
  NSwitch,
  NDynamicTags,
  NButton,
  useMessage,
} from 'naive-ui'
import { useAppStore } from '../store/state'
import { zh } from '../i18n/zh'

const store = useAppStore()
const message = useMessage()

const interval = ref(15)
const excludePorts = ref<string[]>([])
const onlyPublic = ref(false)
const offset = ref(0)

watch(
  () => store.config,
  (c) => {
    if (!c) return
    interval.value = c.scan_interval_sec
    excludePorts.value = c.rules.exclude_ports.map(String)
    onlyPublic.value = c.rules.only_public_bind
    offset.value = c.rules.local_port_offset
  },
  { immediate: true },
)

async function save() {
  await store.updateScanInterval(interval.value)
  await store.updateRules({
    exclude_ports: excludePorts.value.map((s) => Number(s)).filter((n) => !Number.isNaN(n)),
    exclude_ranges: store.config?.rules.exclude_ranges ?? [],
    only_public_bind: onlyPublic.value,
    local_port_offset: offset.value,
  })
  message.success(zh.settings.saved)
}

const hasConfig = computed(() => store.config !== null)
</script>

<template>
  <n-card v-if="hasConfig" :title="zh.settings.title">
    <n-form label-placement="left" label-width="160">
      <n-form-item :label="zh.settings.scanInterval">
        <n-input-number v-model:value="interval" :min="3" :max="3600" />
      </n-form-item>
      <n-form-item :label="zh.settings.excludePorts">
        <n-dynamic-tags v-model:value="excludePorts" />
      </n-form-item>
      <n-form-item :label="zh.settings.onlyPublicBind">
        <n-switch v-model:value="onlyPublic" />
      </n-form-item>
      <n-form-item :label="zh.settings.localPortOffset">
        <n-input-number v-model:value="offset" :min="0" :max="50000" />
      </n-form-item>
      <n-space justify="end">
        <n-button type="primary" @click="save">{{ zh.settings.save }}</n-button>
      </n-space>
    </n-form>
  </n-card>
</template>
