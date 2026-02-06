<template>
  <div
    class="source-card"
    :class="{
      unhealthy: source.status?.state === 'unhealthy',
      disabled: !source.enabled
    }"
  >
    <div class="source-header">
      <div>
        <div class="source-name">{{ source.name }}</div>
        <div class="source-type">{{ source.type }}</div>
      </div>
      <span class="badge" :class="statusBadgeClass">
        {{ statusText }}
      </span>
    </div>

    <div class="source-url">{{ source.base_url }}</div>

    <div class="source-stats">
      <span v-if="source.status?.latency">
        延迟: {{ source.status.latency }}ms
      </span>
      <span v-if="source.status?.balance">
        余额: ${{ source.status.balance.toFixed(2) }}
      </span>
      <span>优先级: {{ source.priority }}</span>
      <span>权重: {{ source.weight }}</span>
    </div>

    <div class="source-caps">
      <span class="cap-tag" :class="{ active: source.capabilities.function_calling }">FC</span>
      <span v-if="source.type !== 'cpa'" class="cap-tag" :class="{ active: source.capabilities.extended_thinking }">Thinking</span>
      <span class="cap-tag" :class="{ active: source.capabilities.vision }">Vision</span>
      <span v-if="source.type === 'cpa' && source.cpa" class="cap-tag active" style="font-size: 10px;">
        {{ source.cpa.providers?.join(', ') || 'CPA' }}
      </span>
    </div>

    <div style="display: flex; gap: 8px; margin-top: 16px;">
      <button class="btn btn-secondary btn-sm" @click="$emit('test', source)">测试</button>
      <button class="btn btn-secondary btn-sm" @click="$emit('edit', source)">编辑</button>
      <button class="btn btn-sm" style="color: var(--danger);" @click="$emit('delete', source)">删除</button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { Source } from '../api'

const props = defineProps<{
  source: Source
}>()

defineEmits<{
  edit: [source: Source]
  delete: [source: Source]
  test: [source: Source]
}>()

const statusBadgeClass = computed(() => {
  if (!props.source.enabled) return 'badge-gray'
  if (props.source.status?.state === 'healthy') return 'badge-success'
  return 'badge-danger'
})

const statusText = computed(() => {
  if (!props.source.enabled) return '已禁用'
  if (props.source.status?.state === 'healthy') return '健康'
  return '异常'
})
</script>
