<template>
  <div>
    <div class="page-header">
      <h1 class="page-title">设置</h1>
    </div>

    <div v-if="loading" class="loading">
      <div class="spinner"></div>
    </div>

    <div v-else>
      <!-- Routing Settings -->
      <div class="card" style="margin-bottom: 24px;">
        <div class="card-header">
          <h3 class="card-title">路由设置</h3>
        </div>

        <div class="form-group">
          <label class="form-label">路由策略</label>
          <select v-model="config.routing.strategy" class="form-select">
            <option value="priority">优先级 (Priority)</option>
            <option value="round-robin">轮询 (Round Robin)</option>
            <option value="weighted">加权 (Weighted)</option>
            <option value="least-latency">最低延迟 (Least Latency)</option>
            <option value="least-cost">最低成本 (Least Cost)</option>
          </select>
          <p style="font-size: 13px; color: var(--gray-500); margin-top: 6px;">
            {{ strategyDescription }}
          </p>
        </div>

        <div class="form-group">
          <label class="checkbox-label">
            <input v-model="config.routing.failover.enabled" type="checkbox" />
            启用故障转移
          </label>
          <p style="font-size: 13px; color: var(--gray-500); margin-top: 6px;">
            当源请求失败时，自动尝试其他可用源
          </p>
        </div>

        <div v-if="config.routing.failover.enabled" class="form-group">
          <label class="form-label">最大重试次数</label>
          <input
            v-model.number="config.routing.failover.max_retries"
            type="number"
            class="form-input"
            min="1"
            max="5"
            style="width: 100px;"
          />
        </div>
      </div>

      <!-- Health Check Settings -->
      <div class="card" style="margin-bottom: 24px;">
        <div class="card-header">
          <h3 class="card-title">健康检查</h3>
        </div>

        <div class="form-group">
          <label class="checkbox-label">
            <input v-model="config.health_check.enabled" type="checkbox" />
            启用健康检查
          </label>
        </div>

        <div v-if="config.health_check.enabled">
          <div style="display: flex; gap: 16px;">
            <div class="form-group" style="flex: 1;">
              <label class="form-label">检查间隔 (秒)</label>
              <input
                v-model.number="config.health_check.interval"
                type="number"
                class="form-input"
                min="10"
              />
            </div>
            <div class="form-group" style="flex: 1;">
              <label class="form-label">超时时间 (秒)</label>
              <input
                v-model.number="config.health_check.timeout"
                type="number"
                class="form-input"
                min="1"
              />
            </div>
            <div class="form-group" style="flex: 1;">
              <label class="form-label">失败阈值</label>
              <input
                v-model.number="config.health_check.failure_threshold"
                type="number"
                class="form-input"
                min="1"
              />
            </div>
          </div>
        </div>
      </div>

      <!-- Server Info -->
      <div class="card" style="margin-bottom: 24px;">
        <div class="card-header">
          <h3 class="card-title">服务器信息</h3>
        </div>
        <div style="display: grid; grid-template-columns: repeat(2, 1fr); gap: 16px;">
          <div>
            <div style="font-size: 13px; color: var(--gray-500);">监听地址</div>
            <div style="font-weight: 500;">{{ config.server?.host }}:{{ config.server?.port }}</div>
          </div>
          <div>
            <div style="font-size: 13px; color: var(--gray-500);">日志保留天数</div>
            <div style="font-weight: 500;">{{ config.logging?.retention_days }} 天</div>
          </div>
        </div>
      </div>

      <!-- Save Button -->
      <div style="display: flex; justify-content: flex-end;">
        <button class="btn btn-primary" :disabled="saving" @click="saveConfig">
          {{ saving ? '保存中...' : '保存设置' }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { configApi } from '../api'

const loading = ref(true)
const saving = ref(false)

const config = ref({
  server: { host: '', port: 0 },
  health_check: {
    enabled: true,
    interval: 60,
    timeout: 10,
    failure_threshold: 3
  },
  routing: {
    strategy: 'priority',
    failover: {
      enabled: true,
      max_retries: 2
    }
  },
  logging: {
    level: 'info',
    retention_days: 7
  }
})

const strategyDescription = computed(() => {
  const descriptions: Record<string, string> = {
    priority: '按优先级选择源，优先级相同时轮询',
    'round-robin': '轮询所有健康的源',
    weighted: '按权重比例分配请求',
    'least-latency': '选择延迟最低的源',
    'least-cost': '选择余额最多的源'
  }
  return descriptions[config.value.routing.strategy] || ''
})

async function fetchConfig() {
  loading.value = true
  try {
    const data = await configApi.get()
    if (data.server) config.value.server = data.server
    if (data.health_check) config.value.health_check = { ...config.value.health_check, ...data.health_check }
    if (data.routing) {
      config.value.routing.strategy = data.routing.strategy || config.value.routing.strategy
      if (data.routing.failover) {
        config.value.routing.failover = { ...config.value.routing.failover, ...data.routing.failover }
      }
    }
    if (data.logging) config.value.logging = { ...config.value.logging, ...data.logging }
  } catch (e) {
    console.error('Failed to fetch config:', e)
  } finally {
    loading.value = false
  }
}

async function saveConfig() {
  saving.value = true
  try {
    await configApi.update({
      routing: config.value.routing,
      health_check: config.value.health_check,
      logging: config.value.logging
    })
    alert('设置已保存')
  } catch (e: any) {
    alert('保存失败: ' + e.message)
  } finally {
    saving.value = false
  }
}

onMounted(() => {
  fetchConfig()
})
</script>
