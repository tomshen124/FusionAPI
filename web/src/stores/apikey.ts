import { defineStore } from 'pinia'
import { ref } from 'vue'
import { keysApi, type APIKey, type KeyLimits } from '../api'

export const useApiKeyStore = defineStore('apikey', () => {
  const keys = ref<APIKey[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function fetchKeys() {
    loading.value = true
    error.value = null
    try {
      keys.value = await keysApi.list()
    } catch (e: any) {
      error.value = e.message
    } finally {
      loading.value = false
    }
  }

  async function createKey(data: { name: string; limits?: KeyLimits; allowed_tools?: string[] }) {
    const created = await keysApi.create(data)
    keys.value.unshift(created)
    return created
  }

  async function updateKey(id: string, data: Partial<APIKey>) {
    const updated = await keysApi.update(id, data)
    const index = keys.value.findIndex(k => k.id === id)
    if (index !== -1) {
      keys.value[index] = updated
    }
    return updated
  }

  async function deleteKey(id: string) {
    await keysApi.delete(id)
    keys.value = keys.value.filter(k => k.id !== id)
  }

  async function rotateKey(id: string) {
    const updated = await keysApi.rotate(id)
    const index = keys.value.findIndex(k => k.id === id)
    if (index !== -1) {
      keys.value[index] = updated
    }
    return updated
  }

  async function blockKey(id: string) {
    const updated = await keysApi.block(id)
    const index = keys.value.findIndex(k => k.id === id)
    if (index !== -1) {
      keys.value[index] = updated
    }
    return updated
  }

  async function unblockKey(id: string) {
    const updated = await keysApi.unblock(id)
    const index = keys.value.findIndex(k => k.id === id)
    if (index !== -1) {
      keys.value[index] = updated
    }
    return updated
  }

  return {
    keys,
    loading,
    error,
    fetchKeys,
    createKey,
    updateKey,
    deleteKey,
    rotateKey,
    blockKey,
    unblockKey
  }
})
