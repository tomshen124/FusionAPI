import { defineStore } from 'pinia'
import { ref } from 'vue'
import { sourcesApi, statusApi, type Source, type SystemStatus } from '../api'

export const useSourceStore = defineStore('source', () => {
  const sources = ref<Source[]>([])
  const status = ref<SystemStatus | null>(null)
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function fetchSources() {
    loading.value = true
    error.value = null
    try {
      sources.value = await sourcesApi.list()
    } catch (e: any) {
      error.value = e.message
    } finally {
      loading.value = false
    }
  }

  async function fetchStatus() {
    try {
      status.value = await statusApi.get()
    } catch (e: any) {
      console.error('Failed to fetch status:', e)
    }
  }

  async function createSource(source: Partial<Source>) {
    const created = await sourcesApi.create(source)
    sources.value.push(created)
    return created
  }

  async function updateSource(id: string, source: Partial<Source>) {
    const updated = await sourcesApi.update(id, source)
    const index = sources.value.findIndex(s => s.id === id)
    if (index !== -1) {
      sources.value[index] = updated
    }
    return updated
  }

  async function deleteSource(id: string) {
    await sourcesApi.delete(id)
    sources.value = sources.value.filter(s => s.id !== id)
  }

  async function testSource(id: string) {
    return await sourcesApi.test(id)
  }

  return {
    sources,
    status,
    loading,
    error,
    fetchSources,
    fetchStatus,
    createSource,
    updateSource,
    deleteSource,
    testSource
  }
})
