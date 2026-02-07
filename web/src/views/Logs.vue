<template>
  <div>
    <div class="page-header">
      <h1 class="page-title">请求日志</h1>
    </div>

    <!-- Filters -->
    <div class="card" style="margin-bottom: 16px;">
      <div class="filters">
        <select v-model="filters.source_id" class="form-select" @change="fetchLogs">
          <option value="">所有源</option>
          <option v-for="source in sources" :key="source.id" :value="source.id">
            {{ source.name }}
          </option>
        </select>
        <input
          v-model="filters.model"
          type="text"
          class="form-input"
          placeholder="模型名称"
          @keyup.enter="fetchLogs"
        />
        <select v-model="filters.success" class="form-select" @change="fetchLogs">
          <option value="">所有状态</option>
          <option value="true">成功</option>
          <option value="false">失败</option>
        </select>
        <select v-model="filters.client_tool" class="form-select" @change="fetchLogs">
          <option value="">所有工具</option>
          <option value="cursor">Cursor</option>
          <option value="claude-code">Claude Code</option>
          <option value="codex-cli">Codex CLI</option>
          <option value="continue">Continue</option>
          <option value="copilot">Copilot</option>
          <option value="openai-sdk">OpenAI SDK</option>
          <option value="anthropic-sdk">Anthropic SDK</option>
          <option value="unknown">Unknown</option>
        </select>
        <select v-model="filters.fc_compat" class="form-select" @change="fetchLogs">
          <option value="">FC 兼容</option>
          <option value="true">是</option>
          <option value="false">否</option>
        </select>
        <button class="btn btn-secondary" @click="fetchLogs">刷新</button>
      </div>
    </div>

    <!-- Logs Table -->
    <div class="card">
      <div v-if="loading" class="loading">
        <div class="spinner"></div>
      </div>

      <table v-else class="table">
        <thead>
          <tr>
            <th>时间</th>
            <th>模型</th>
            <th>源</th>
            <th>工具</th>
            <th>Key</th>
            <th>状态</th>
            <th>FC兼容</th>
            <th>延迟</th>
            <th>Tokens</th>
            <th>特性</th>
            <th>错误</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="log in logs" :key="log.id">
            <td>{{ formatTime(log.timestamp) }}</td>
            <td>{{ log.model }}</td>
            <td>{{ log.source_name || '-' }}</td>
            <td>
              <span v-if="log.client_tool && log.client_tool !== 'unknown'" class="badge badge-gray">
                {{ log.client_tool }}
              </span>
              <span v-else>-</span>
            </td>
            <td>{{ log.api_key_id ? log.api_key_id.substring(0, 8) + '...' : '-' }}</td>
            <td>
              <span class="badge" :class="log.success ? 'badge-success' : 'badge-danger'">
                {{ log.success ? '成功' : `失败 (${log.status_code})` }}
              </span>
            </td>
            <td>
              <span v-if="log.fc_compat_used" class="badge badge-gray">兼容层</span>
              <span v-else>-</span>
            </td>
            <td>{{ log.latency_ms }}ms</td>
            <td>{{ log.total_tokens || '-' }}</td>
            <td>
              <span v-if="log.has_tools" class="cap-tag active">FC</span>
              <span v-if="log.has_thinking" class="cap-tag active">Thinking</span>
              <span v-if="log.stream" class="cap-tag">Stream</span>
              <span v-if="log.failover_from" class="cap-tag" style="background: #fef3c7; color: #92400e;">
                Failover
              </span>
            </td>
            <td style="max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">
              {{ log.error || '-' }}
            </td>
          </tr>
          <tr v-if="logs.length === 0">
            <td colspan="11" style="text-align: center; color: var(--gray-500);">
              暂无日志记录
            </td>
          </tr>
        </tbody>
      </table>

      <!-- Pagination -->
      <div v-if="logs.length > 0" style="display: flex; justify-content: center; gap: 8px; margin-top: 16px;">
        <button class="btn btn-secondary btn-sm" :disabled="offset === 0" @click="prevPage">上一页</button>
        <span style="line-height: 32px; color: var(--gray-500);">
          第 {{ Math.floor(offset / limit) + 1 }} 页
        </span>
        <button class="btn btn-secondary btn-sm" :disabled="logs.length < limit" @click="nextPage">下一页</button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useSourceStore } from '../stores/source'
import { logsApi, type RequestLog } from '../api'

const sourceStore = useSourceStore()
const logs = ref<RequestLog[]>([])
const loading = ref(false)
const limit = 20
const offset = ref(0)

const filters = ref({
  source_id: '',
  model: '',
  success: '' as '' | 'true' | 'false',
  client_tool: '',
  fc_compat: '' as '' | 'true' | 'false',
})

const sources = computed(() => sourceStore.sources)

async function fetchLogs() {
  loading.value = true
  try {
    const params: any = {
      limit,
      offset: offset.value
    }
    if (filters.value.source_id) params.source_id = filters.value.source_id
    if (filters.value.model) params.model = filters.value.model
    if (filters.value.success) params.success = filters.value.success === 'true'
    if (filters.value.client_tool) params.client_tool = filters.value.client_tool
    if (filters.value.fc_compat) params.fc_compat = filters.value.fc_compat === 'true'

    logs.value = await logsApi.list(params)
  } catch (e) {
    console.error('Failed to fetch logs:', e)
  } finally {
    loading.value = false
  }
}

function prevPage() {
  offset.value = Math.max(0, offset.value - limit)
  fetchLogs()
}

function nextPage() {
  offset.value += limit
  fetchLogs()
}

function formatTime(timestamp: string) {
  const date = new Date(timestamp)
  return date.toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit'
  })
}

onMounted(() => {
  sourceStore.fetchSources()
  fetchLogs()
})
</script>
