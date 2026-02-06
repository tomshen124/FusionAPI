<template>
  <div>
    <div class="page-header" style="display: flex; justify-content: space-between; align-items: center;">
      <h1 class="page-title">源管理</h1>
      <button class="btn btn-primary" @click="openModal()">+ 添加源</button>
    </div>

    <div v-if="loading" class="loading">
      <div class="spinner"></div>
    </div>

    <div v-else-if="sources.length === 0" class="empty-state card">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <circle cx="12" cy="12" r="10"/>
        <line x1="12" y1="8" x2="12" y2="12"/>
        <line x1="12" y1="16" x2="12.01" y2="16"/>
      </svg>
      <p>暂无源配置</p>
      <button class="btn btn-primary" style="margin-top: 16px;" @click="openModal()">添加第一个源</button>
    </div>

    <div v-else class="source-grid">
      <SourceCard
        v-for="source in sources"
        :key="source.id"
        :source="source"
        @edit="openModal(source)"
        @delete="handleDelete(source)"
        @test="handleTest(source)"
      />
    </div>

    <!-- Modal -->
    <div v-if="showModal" class="modal-overlay" @click.self="closeModal">
      <div class="modal">
        <div class="modal-header">
          <h3 class="modal-title">{{ editingSource ? '编辑源' : '添加源' }}</h3>
          <button class="modal-close" @click="closeModal">&times;</button>
        </div>
        <div class="modal-body">
          <SourceForm
            :source="editingSource"
            @submit="handleSubmit"
            @cancel="closeModal"
          />
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useSourceStore } from '../stores/source'
import type { Source } from '../api'
import SourceCard from '../components/SourceCard.vue'
import SourceForm from '../components/SourceForm.vue'

const sourceStore = useSourceStore()
const showModal = ref(false)
const editingSource = ref<Source | null>(null)

const sources = computed(() => sourceStore.sources)
const loading = computed(() => sourceStore.loading)

function openModal(source?: Source) {
  editingSource.value = source || null
  showModal.value = true
}

function closeModal() {
  showModal.value = false
  editingSource.value = null
}

async function handleSubmit(data: Partial<Source>) {
  try {
    if (editingSource.value) {
      await sourceStore.updateSource(editingSource.value.id, data)
    } else {
      await sourceStore.createSource(data)
    }
    closeModal()
  } catch (e: any) {
    alert('操作失败: ' + e.message)
  }
}

async function handleDelete(source: Source) {
  if (!confirm(`确定要删除源 "${source.name}" 吗？`)) return
  try {
    await sourceStore.deleteSource(source.id)
  } catch (e: any) {
    alert('删除失败: ' + e.message)
  }
}

async function handleTest(source: Source) {
  try {
    const result = await sourceStore.testSource(source.id)
    if (result.success) {
      alert('连接测试成功！')
    } else {
      alert('连接测试失败: ' + result.error)
    }
  } catch (e: any) {
    alert('测试失败: ' + e.message)
  }
}

onMounted(() => {
  sourceStore.fetchSources()
})
</script>
