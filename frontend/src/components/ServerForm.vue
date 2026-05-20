<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import {
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NRadio,
  NRadioGroup,
  NSwitch,
  NButton,
  NSpace,
} from 'naive-ui'
import type { Server } from '../types'
import { zh } from '../i18n/zh'

const props = defineProps<{ initial?: Partial<Server> }>()
const emit = defineEmits<{ submit: [s: Server]; cancel: [] }>()

function blank(): Server {
  return {
    id: '',
    name: '',
    host: '',
    port: 22,
    user: '',
    auth_method: 'ssh_agent',
    host_key: 'known_hosts',
    enabled: true,
  }
}

const form = ref<Server>({ ...blank(), ...(props.initial as Server | undefined) })
watch(
  () => props.initial,
  (v) => {
    form.value = { ...blank(), ...(v as Server | undefined) }
  },
)

const isPassword = computed(() => form.value.auth_method === 'password')
const isKey = computed(() => form.value.auth_method === 'ssh_key')

function submit() {
  emit('submit', { ...form.value })
}
</script>

<template>
  <n-form label-placement="left" label-width="100" require-mark-placement="right-hanging">
    <n-form-item :label="zh.form.name">
      <n-input v-model:value="form.name" placeholder="ubt / 测试机" />
    </n-form-item>
    <n-form-item :label="zh.form.host">
      <n-input v-model:value="form.host" placeholder="10.0.0.42" />
    </n-form-item>
    <n-form-item :label="zh.form.port">
      <n-input-number v-model:value="form.port" :min="1" :max="65535" />
    </n-form-item>
    <n-form-item :label="zh.form.user">
      <n-input v-model:value="form.user" placeholder="ubuntu" />
    </n-form-item>
    <n-form-item :label="zh.form.auth_method">
      <n-radio-group v-model:value="form.auth_method">
        <n-radio value="ssh_agent">{{ zh.form.authAgent }}</n-radio>
        <n-radio value="ssh_key">{{ zh.form.authKey }}</n-radio>
        <n-radio value="password">{{ zh.form.authPassword }}</n-radio>
      </n-radio-group>
    </n-form-item>
    <n-form-item v-if="isPassword" :label="zh.form.password">
      <n-input
        v-model:value="form.password"
        type="password"
        show-password-on="mousedown"
      />
    </n-form-item>
    <n-form-item v-if="isKey" :label="zh.form.key_path">
      <n-input v-model:value="form.key_path" placeholder="~/.ssh/id_ed25519" />
    </n-form-item>
    <n-form-item v-if="isKey" :label="zh.form.passphrase">
      <n-input
        v-model:value="form.passphrase"
        type="password"
        show-password-on="mousedown"
      />
    </n-form-item>
    <n-form-item :label="zh.form.host_key">
      <n-radio-group v-model:value="form.host_key">
        <n-radio value="known_hosts">{{ zh.form.hostKeyStrict }}</n-radio>
        <n-radio value="insecure">{{ zh.form.hostKeyInsecure }}</n-radio>
      </n-radio-group>
    </n-form-item>
    <n-form-item :label="zh.form.enabled">
      <n-switch v-model:value="form.enabled" />
    </n-form-item>
    <n-space justify="end">
      <n-button @click="emit('cancel')">{{ zh.form.cancel }}</n-button>
      <n-button type="primary" @click="submit">{{ zh.form.save }}</n-button>
    </n-space>
  </n-form>
</template>
