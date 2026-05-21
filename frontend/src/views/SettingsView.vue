<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import {
  NSpace,
  NCard,
  NForm,
  NFormItem,
  NInputNumber,
  NDynamicTags,
  NButton,
  NText,
  useMessage,
} from 'naive-ui'
import { useAppStore } from '../store/state'
import { zh } from '../i18n/zh'

const store = useAppStore()
const message = useMessage()

const interval = ref<number>(15)
const excludePorts = ref<string[]>([])

watch(
  () => store.config,
  (c) => {
    if (!c) return
    interval.value = c.scan_interval_sec
    excludePorts.value = (c.rules.exclude_ports ?? []).map(String)
  },
  { immediate: true },
)

async function save() {
  await store.updateScanInterval(interval.value)
  await store.updateRules({
    exclude_ports: excludePorts.value
      .map((s) => Number(s))
      .filter((n) => !Number.isNaN(n) && n > 0),
    exclude_ranges: store.config?.rules.exclude_ranges ?? [],
  })
  message.success(zh.settings.saved)
}

const hasConfig = computed(() => store.config !== null)
</script>

<template>
  <n-space vertical :size="16">
    <n-card v-if="hasConfig" :title="zh.settings.title">
      <n-form label-placement="left" label-width="160">
        <n-form-item :label="zh.settings.scanInterval">
          <n-space vertical :size="4" style="width: 100%">
            <n-input-number
              v-model:value="interval"
              :min="3"
              :max="3600"
              :placeholder="zh.settings.scanIntervalDefault"
              style="width: 160px"
            />
            <n-text depth="3" style="font-size: 11px">
              {{ zh.settings.scanIntervalDefault }}
            </n-text>
          </n-space>
        </n-form-item>
        <n-form-item :label="zh.settings.excludePorts">
          <n-space vertical :size="4" style="width: 100%">
            <n-dynamic-tags v-model:value="excludePorts" />
            <n-text depth="3" style="font-size: 11px">
              {{ zh.settings.excludePortsDefault }}
            </n-text>
          </n-space>
        </n-form-item>
        <n-space justify="end">
          <n-button type="primary" @click="save">{{ zh.settings.save }}</n-button>
        </n-space>
      </n-form>
    </n-card>

    <n-card :title="zh.settings.sshConfig">
      <n-text depth="3" style="font-size: 12px">
        {{ zh.settings.sshConfigDesc }}
      </n-text>
    </n-card>
  </n-space>
</template>
