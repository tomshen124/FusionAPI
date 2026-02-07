<template>
  <div>
    <div class="page-header">
      <h1 class="page-title">API Keys</h1>
      <button class="btn btn-primary" @click="showCreateForm = true">新建 Key</button>
    </div>

    <!-- Create Form Modal -->
    <div v-if="showCreateForm" class="modal-overlay" @click.self="showCreateForm = false">
      <div class="card modal-card">
        <div class="card-header">
          <h3 class="card-title">新建 API Key</h3>
        </div>
        <div class="form-group">
          <label>备注名</label>
          <input v-model="newKey.name" class="form-input" placeholder="例：Cursor 开发机" />
        </div>
        <div class="form-group">
          <label>RPM 限制 (0=无限)</label>
          <input v-model.number="newKey.limits.rpm" class="form-input" type="number" min="0" />
        </div>
        <div class="form-group">
          <label>日配额 (0=无限)</label>
          <input v-model.number="newKey.limits.daily_quota" class="form-input" type="number" min="0" />
        </div>
        <div class="form-group">
          <label>并发限制 (0=无限)</label>
          <input v-model.number="newKey.limits.concurrent" class="form-input" type="number" min="0" />
        </div>
        <div class="form-group">
          <label>允许工具 (留空=所有)</label>
          <input v-model="newKey.allowed_tools_str" class="form-input" placeholder="cursor,claude-code,codex-cli" />
        </div>
        <div class="form-group">
          <label>工具配额 (JSON, 留空=不限)</label>
          <input v-model="newKey.tool_quotas_str" class="form-input" placeholder='{"cursor":100,"claude-code":200}' />
        </div>
        <div style="display:flex;gap:8px;justify-content:flex-end;margin-top:16px;">
          <button class="btn btn-secondary" @click="showCreateForm = false">取消</button>
          <button class="btn btn-primary" @click="handleCreate" :disabled="!newKey.name">创建</button>
        </div>
      </div>
    </div>

    <!-- Edit Modal -->
    <div v-if="showEditForm" class="modal-overlay" @click.self="showEditForm = false">
      <div class="card modal-card">
        <div class="card-header">
          <h3 class="card-title">编辑 API Key</h3>
        </div>
        <div class="form-group">
          <label>备注名</label>
          <input v-model="editKey.name" class="form-input" />
        </div>
        <div class="form-group">
          <label>RPM 限制 (0=无限)</label>
          <input v-model.number="editKey.limits.rpm" class="form-input" type="number" min="0" />
        </div>
        <div class="form-group">
          <label>日配额 (0=无限)</label>
          <input v-model.number="editKey.limits.daily_quota" class="form-input" type="number" min="0" />
        </div>
        <div class="form-group">
          <label>并发限制 (0=无限)</label>
          <input v-model.number="editKey.limits.concurrent" class="form-input" type="number" min="0" />
        </div>
        <div class="form-group">
          <label>允许工具 (留空=所有)</label>
          <input v-model="editKey.allowed_tools_str" class="form-input" placeholder="cursor,claude-code,codex-cli" />
        </div>
        <div class="form-group">
          <label>工具配额 (JSON, 留空=不限)</label>
          <input v-model="editKey.tool_quotas_str" class="form-input" placeholder='{"cursor":100,"claude-code":200}' />
        </div>
        <div style="display:flex;gap:8px;justify-content:flex-end;margin-top:16px;">
          <button class="btn btn-secondary" @click="showEditForm = false">取消</button>
          <button class="btn btn-primary" @click="handleUpdate" :disabled="!editKey.name">保存</button>
        </div>
      </div>
    </div>

    <!-- Created Key Display (show once after creation) -->
    <div v-if="createdKeyValue" class="card" style="margin-bottom:16px;border:2px solid var(--success);">
      <div style="display:flex;align-items:center;gap:12px;">
        <span style="color:var(--success);font-weight:600;">Key 已创建！请立即复制，此后无法再次查看完整 Key。</span>
      </div>
      <div style="margin-top:8px;display:flex;align-items:center;gap:8px;">
        <code style="flex:1;padding:8px;background:var(--gray-100);border-radius:4px;font-size:14px;">{{ createdKeyValue }}</code>
        <button class="btn btn-secondary btn-sm" @click="copyKey(createdKeyValue)">复制</button>
      </div>
    </div>

    <!-- Keys Table -->
    <div class="card">
      <div v-if="store.loading" class="loading"><div class="spinner"></div></div>
      <table v-else class="table">
        <thead>
          <tr>
            <th>名称</th>
            <th>Key</th>
            <th>状态</th>
            <th>RPM</th>
            <th>日配额</th>
            <th>并发</th>
            <th>今日用量</th>
            <th>创建时间</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="key in store.keys" :key="key.id">
            <td>{{ key.name || '-' }}</td>
            <td><code>{{ maskKey(key.key) }}</code></td>
            <td>
              <span class="badge" :class="key.enabled ? 'badge-success' : 'badge-danger'">
                {{ key.enabled ? '启用' : '已封禁' }}
              </span>
            </td>
            <td>{{ key.limits?.rpm || '无限' }}</td>
            <td>{{ key.limits?.daily_quota || '无限' }}</td>
            <td>{{ key.limits?.concurrent || '无限' }}</td>
            <td>{{ key.daily_usage ?? '-' }}</td>
            <td>{{ formatTime(key.created_at) }}</td>
            <td>
              <div style="display:flex;gap:4px;">
                <button class="btn btn-secondary btn-sm" @click="startEdit(key)">编辑</button>
                <button class="btn btn-secondary btn-sm" @click="showStats(key)">统计</button>
                <button v-if="key.enabled" class="btn btn-danger btn-sm" @click="handleBlock(key.id)">封禁</button>
                <button v-else class="btn btn-success btn-sm" @click="handleUnblock(key.id)">解封</button>
                <button class="btn btn-secondary btn-sm" @click="handleRotate(key.id)">轮换</button>
                <button class="btn btn-danger btn-sm" @click="handleDelete(key.id)">删除</button>
              </div>
            </td>
          </tr>
          <tr v-if="store.keys.length === 0">
            <td colspan="9" style="text-align:center;color:var(--gray-500);">暂无 API Key</td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Stats Modal -->
    <div v-if="showStatsModal" class="modal-overlay" @click.self="showStatsModal = false">
      <div class="card modal-card" style="width:600px;">
        <div class="card-header">
          <h3 class="card-title">使用统计 - {{ statsKeyName }}</h3>
        </div>
        <div v-if="usageStats.length > 0" style="padding:16px;">
          <table class="table">
            <thead>
              <tr>
                <th>日期</th>
                <th>请求数</th>
                <th>成功</th>
                <th>失败</th>
                <th>Tokens</th>
                <th>平均延迟</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="stat in usageStats" :key="stat.date">
                <td>{{ stat.date }}</td>
                <td>{{ stat.request_count }}</td>
                <td style="color:var(--success)">{{ stat.success_count }}</td>
                <td style="color:var(--danger)">{{ stat.fail_count }}</td>
                <td>{{ stat.total_tokens }}</td>
                <td>{{ stat.avg_latency_ms?.toFixed(0) || 0 }}ms</td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-else style="padding:24px;text-align:center;color:var(--gray-500);">
          暂无使用数据
        </div>
        <div style="display:flex;justify-content:flex-end;padding:0 16px 16px;">
          <button class="btn btn-secondary" @click="showStatsModal = false">关闭</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { useApiKeyStore } from '../stores/apikey'
import { keysApi, type APIKey, type KeyDailyUsage } from '../api'

const store = useApiKeyStore()
const showCreateForm = ref(false)
const showEditForm = ref(false)
const createdKeyValue = ref('')
const showStatsModal = ref(false)
const statsKeyName = ref('')
const usageStats = ref<KeyDailyUsage[]>([])

const newKey = reactive({
  name: '',
  limits: { rpm: 0, daily_quota: 0, concurrent: 0 },
  allowed_tools_str: '',
  tool_quotas_str: ''
})

const editKey = reactive({
  id: '',
  name: '',
  limits: { rpm: 0, daily_quota: 0, concurrent: 0 },
  allowed_tools_str: '',
  tool_quotas_str: ''
})

function maskKey(key: string) {
  if (!key) return '-'
  if (key.length <= 12) return key
  return key.substring(0, 8) + '...' + key.substring(key.length - 4)
}

function formatTime(timestamp: string) {
  if (!timestamp) return '-'
  const date = new Date(timestamp)
  return date.toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit'
  })
}

async function copyKey(key: string) {
  try {
    await navigator.clipboard.writeText(key)
  } catch {
    // fallback
    const ta = document.createElement('textarea')
    ta.value = key
    document.body.appendChild(ta)
    ta.select()
    document.execCommand('copy')
    document.body.removeChild(ta)
  }
}

async function handleCreate() {
  try {
    const allowed_tools = newKey.allowed_tools_str
      ? newKey.allowed_tools_str.split(',').map(s => s.trim()).filter(Boolean)
      : []
    let tool_quotas: Record<string, number> | undefined
    if (newKey.tool_quotas_str) {
      try {
        tool_quotas = JSON.parse(newKey.tool_quotas_str)
      } catch {
        alert('工具配额 JSON 格式错误')
        return
      }
    }
    const created = await store.createKey({
      name: newKey.name,
      limits: { ...newKey.limits, tool_quotas },
      allowed_tools
    })
    createdKeyValue.value = created.key
    showCreateForm.value = false
    newKey.name = ''
    newKey.limits = { rpm: 0, daily_quota: 0, concurrent: 0 }
    newKey.allowed_tools_str = ''
    newKey.tool_quotas_str = ''
  } catch (e: any) {
    alert('创建失败: ' + e.message)
  }
}

function startEdit(key: APIKey) {
  editKey.id = key.id
  editKey.name = key.name
  editKey.limits = {
    rpm: key.limits?.rpm || 0,
    daily_quota: key.limits?.daily_quota || 0,
    concurrent: key.limits?.concurrent || 0
  }
  editKey.allowed_tools_str = (key.allowed_tools || []).join(',')
  editKey.tool_quotas_str = (key.limits as any)?.tool_quotas ? JSON.stringify((key.limits as any).tool_quotas) : ''
  showEditForm.value = true
}

async function handleUpdate() {
  try {
    const allowed_tools = editKey.allowed_tools_str
      ? editKey.allowed_tools_str.split(',').map(s => s.trim()).filter(Boolean)
      : []
    let tool_quotas: Record<string, number> | undefined
    if (editKey.tool_quotas_str) {
      try {
        tool_quotas = JSON.parse(editKey.tool_quotas_str)
      } catch {
        alert('工具配额 JSON 格式错误')
        return
      }
    }
    await store.updateKey(editKey.id, {
      name: editKey.name,
      limits: { ...editKey.limits, tool_quotas },
      allowed_tools
    })
    showEditForm.value = false
  } catch (e: any) {
    alert('更新失败: ' + e.message)
  }
}

async function handleBlock(id: string) {
  try {
    await store.blockKey(id)
  } catch (e: any) {
    alert('封禁失败: ' + e.message)
  }
}

async function handleUnblock(id: string) {
  try {
    await store.unblockKey(id)
  } catch (e: any) {
    alert('解封失败: ' + e.message)
  }
}

async function handleRotate(id: string) {
  if (!confirm('轮换将生成新 Key，旧 Key 立即失效。确定继续？')) return
  try {
    const updated = await store.rotateKey(id)
    createdKeyValue.value = updated.key
  } catch (e: any) {
    alert('轮换失败: ' + e.message)
  }
}

async function handleDelete(id: string) {
  if (!confirm('确定删除此 API Key？此操作不可撤销。')) return
  try {
    await store.deleteKey(id)
  } catch (e: any) {
    alert('删除失败: ' + e.message)
  }
}

async function showStats(key: APIKey) {
  statsKeyName.value = key.name || key.id
  try {
    usageStats.value = await keysApi.getUsage(key.id)
    showStatsModal.value = true
  } catch (e: any) {
    alert('获取统计失败: ' + e.message)
  }
}

onMounted(() => {
  store.fetchKeys()
})
</script>

<style scoped>
.modal-overlay {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
}

.modal-card {
  width: 480px;
  max-width: 90vw;
  max-height: 90vh;
  overflow-y: auto;
}

.form-group {
  margin-bottom: 12px;
}

.form-group label {
  display: block;
  margin-bottom: 4px;
  font-size: 14px;
  font-weight: 500;
  color: var(--gray-700);
}

.btn-success {
  background: var(--success);
  color: white;
  border: none;
  padding: 6px 12px;
  border-radius: 6px;
  cursor: pointer;
  font-size: 13px;
}

.btn-success:hover {
  opacity: 0.9;
}
</style>
