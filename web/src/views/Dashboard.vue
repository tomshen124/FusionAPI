<template>
  <div>
    <div class="page-header">
      <h1 class="page-title">仪表盘</h1>
    </div>

    <!-- Stats Cards -->
    <div class="stats-grid">
      <div class="stat-card">
        <div class="stat-label">活跃源</div>
        <div class="stat-value">
          {{ status?.healthy_sources || 0 }}/{{ status?.total_sources || 0 }}
        </div>
      </div>
      <div class="stat-card">
        <div class="stat-label">今日请求</div>
        <div class="stat-value">{{ todayStats?.total_requests || 0 }}</div>
      </div>
      <div class="stat-card">
        <div class="stat-label">成功率</div>
        <div class="stat-value" :class="successRateClass">
          {{ todayStats?.success_rate?.toFixed(1) || 0 }}%
        </div>
      </div>
      <div class="stat-card">
        <div class="stat-label">平均延迟</div>
        <div class="stat-value">{{ todayStats?.avg_latency_ms?.toFixed(0) || 0 }}ms</div>
      </div>
    </div>

    <!-- Sources Status -->
    <div class="card" style="margin-bottom: 24px;">
      <div class="card-header">
        <h3 class="card-title">源状态</h3>
      </div>
      <table class="table">
        <thead>
          <tr>
            <th>名称</th>
            <th>类型</th>
            <th>状态</th>
            <th>延迟</th>
            <th>余额</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="source in sources" :key="source.id">
            <td>{{ source.name }}</td>
            <td>
              <span class="badge badge-gray">{{ source.type }}</span>
            </td>
            <td>
              <span class="badge" :class="getStatusBadgeClass(source)">
                {{ getStatusText(source) }}
              </span>
            </td>
            <td>{{ source.status?.latency || 0 }}ms</td>
            <td>{{ source.status?.balance ? `$${source.status.balance.toFixed(2)}` : '-' }}</td>
          </tr>
          <tr v-if="sources.length === 0">
            <td colspan="5" style="text-align: center; color: var(--gray-500);">
              暂无源配置
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Recent Requests -->
    <div class="card">
      <div class="card-header">
        <h3 class="card-title">最近请求</h3>
        <router-link to="/logs" class="btn btn-secondary btn-sm">查看全部</router-link>
      </div>
      <table class="table">
        <thead>
          <tr>
            <th>时间</th>
            <th>模型</th>
            <th>源</th>
            <th>状态</th>
            <th>延迟</th>
            <th>特性</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="log in recentLogs" :key="log.id">
            <td>{{ formatTime(log.timestamp) }}</td>
            <td>{{ log.model }}</td>
            <td>{{ log.source_name }}</td>
            <td>
              <span class="badge" :class="log.success ? 'badge-success' : 'badge-danger'">
                {{ log.success ? '成功' : '失败' }}
              </span>
            </td>
            <td>{{ log.latency_ms }}ms</td>
            <td>
              <span v-if="log.has_tools" class="cap-tag active">FC</span>
              <span v-if="log.has_thinking" class="cap-tag active">Thinking</span>
              <span v-if="log.stream" class="cap-tag">Stream</span>
            </td>
          </tr>
          <tr v-if="recentLogs.length === 0">
            <td colspan="6" style="text-align: center; color: var(--gray-500);">
              暂无请求记录
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useSourceStore } from '../stores/source'
import { logsApi, statsApi, type RequestLog, type Stats } from '../api'

const sourceStore = useSourceStore()
const stats = ref<Stats | null>(null)
const recentLogs = ref<RequestLog[]>([])

const sources = computed(() => sourceStore.sources)
const status = computed(() => sourceStore.status)

const todayStats = computed(() => {
  if (!stats.value?.daily?.length) return null
  return stats.value.daily[0]
})

const successRateClass = computed(() => {
  const rate = todayStats.value?.success_rate || 0
  if (rate >= 99) return 'success'
  if (rate >= 95) return 'warning'
  return 'danger'
})

function getStatusBadgeClass(source: any) {
  if (!source.enabled) return 'badge-gray'
  if (source.status?.state === 'healthy') return 'badge-success'
  return 'badge-danger'
}

function getStatusText(source: any) {
  if (!source.enabled) return '已禁用'
  if (source.status?.state === 'healthy') return '健康'
  return '异常'
}

function formatTime(timestamp: string) {
  const date = new Date(timestamp)
  return date.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

onMounted(async () => {
  await Promise.all([
    sourceStore.fetchSources(),
    sourceStore.fetchStatus()
  ])

  try {
    stats.value = await statsApi.get()
    recentLogs.value = await logsApi.list({ limit: 10 })
  } catch (e) {
    console.error('Failed to fetch data:', e)
  }
})
</script>
