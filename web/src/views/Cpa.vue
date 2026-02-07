<template>
  <div>
    <div class="page-header">
      <h1 class="page-title">CPA 反代</h1>
      <p class="page-subtitle">统一入口走 FusionAPI，CPA 作为上游源参与路由与故障切换。</p>
    </div>

    <div class="card" style="margin-bottom: 24px;">
      <div class="card-header">
        <h3 class="card-title">统一反代入口</h3>
        <button class="btn btn-secondary btn-sm" @click="copyProxyBase">复制地址</button>
      </div>
      <div class="endpoint">{{ proxyBase }}</div>
      <p class="hint">常用接口：`/v1/chat/completions`、`/v1/models`</p>
      <div class="code-block">
        <pre><code>curl {{ proxyBase }}/models</code></pre>
      </div>
    </div>

    <div class="card">
      <div class="card-header">
        <h3 class="card-title">已配置 CPA 源</h3>
        <router-link to="/sources" class="btn btn-primary btn-sm">去源管理</router-link>
      </div>

      <div v-if="loading" class="loading">
        <div class="spinner"></div>
      </div>

      <table v-else-if="cpaSources.length > 0" class="table">
        <thead>
          <tr>
            <th>名称</th>
            <th>上游地址</th>
            <th>Provider</th>
            <th>模式</th>
            <th>模型映射</th>
            <th>状态</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="source in cpaSources" :key="source.id">
            <td>{{ source.name }}</td>
            <td class="url">{{ source.base_url }}</td>
            <td>{{ formatProviders(source) }}</td>
            <td>{{ source.cpa?.account_mode || '-' }}</td>
            <td>
              <div v-if="source.status?.model_providers && Object.keys(source.status.model_providers).length > 0">
                <span v-for="[model, provider] in Object.entries(source.status.model_providers).slice(0, 5)" :key="model"
                  class="badge badge-gray" style="margin:2px;">
                  {{ model.split('/').pop() }} → {{ provider }}
                </span>
                <span v-if="Object.keys(source.status.model_providers).length > 5"
                  style="color:var(--gray-500);font-size:12px;">
                  +{{ Object.keys(source.status.model_providers).length - 5 }} 更多
                </span>
              </div>
              <span v-else>-</span>
            </td>
            <td>
              <span class="badge" :class="getStatusClass(source)">
                {{ getStatusText(source) }}
              </span>
            </td>
          </tr>
        </tbody>
      </table>

      <div v-else class="empty-state">
        <p>还没有 CPA 源。请在“源管理”里添加类型为 <code>cpa</code> 的源。</p>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useSourceStore } from '../stores/source'
import type { Source } from '../api'

const sourceStore = useSourceStore()

const loading = computed(() => sourceStore.loading)
const cpaSources = computed(() => sourceStore.sources.filter(s => s.type === 'cpa'))

const origin = computed(() => {
  if (typeof window === 'undefined') return 'http://localhost:18080'
  return window.location.origin
})

const proxyBase = computed(() => `${origin.value}/v1`)

function formatProviders(source: Source) {
  const providers = source.cpa?.providers || []
  return providers.length ? providers.join(', ') : '未设置'
}

function getStatusClass(source: Source) {
  if (!source.enabled) return 'badge-gray'
  if (source.status?.state === 'healthy') return 'badge-success'
  return 'badge-danger'
}

function getStatusText(source: Source) {
  if (!source.enabled) return '已禁用'
  if (source.status?.state === 'healthy') return '健康'
  return '异常'
}

async function copyProxyBase() {
  try {
    await navigator.clipboard.writeText(proxyBase.value)
    alert('已复制: ' + proxyBase.value)
  } catch {
    alert('复制失败，请手动复制: ' + proxyBase.value)
  }
}

onMounted(() => {
  sourceStore.fetchSources()
})
</script>

<style scoped>
.page-subtitle {
  margin-top: 8px;
  color: var(--gray-500);
  font-size: 14px;
}

.endpoint {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 18px;
  font-weight: 600;
  color: var(--gray-900);
}

.hint {
  margin-top: 8px;
  font-size: 13px;
  color: var(--gray-500);
}

.code-block {
  margin-top: 12px;
  background: var(--gray-900);
  color: #e5e7eb;
  border-radius: 8px;
  padding: 12px;
  overflow-x: auto;
}

.code-block pre {
  margin: 0;
}

.url {
  max-width: 360px;
  word-break: break-all;
}
</style>
