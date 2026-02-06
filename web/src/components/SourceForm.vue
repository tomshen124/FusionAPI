<template>
  <form @submit.prevent="handleSubmit">
    <div class="form-group">
      <label class="form-label">名称 *</label>
      <input v-model="form.name" type="text" class="form-input" required placeholder="例如：中转站A" />
    </div>

    <div class="form-group">
      <label class="form-label">类型 *</label>
      <select v-model="form.type" class="form-select" required>
        <option value="newapi">NewAPI</option>
        <option value="cpa">CPA</option>
        <option value="openai">OpenAI</option>
        <option value="anthropic">Anthropic</option>
        <option value="custom">Custom</option>
      </select>
    </div>

    <div class="form-group">
      <label class="form-label">Base URL *</label>
      <input v-model="form.base_url" type="url" class="form-input" required placeholder="https://api.example.com" />
    </div>

    <div class="form-group">
      <label class="form-label">API Key {{ form.type === 'cpa' ? '(可选)' : (source ? '' : '*') }}</label>
      <input
        v-model="form.api_key"
        type="password"
        class="form-input"
        :required="!source && form.type !== 'cpa'"
        :placeholder="source ? '留空保持不变' : (form.type === 'cpa' ? '可选，留空则不发送认证头' : 'sk-xxxxxxxx')"
      />
    </div>

    <div style="display: flex; gap: 16px;">
      <div class="form-group" style="flex: 1;">
        <label class="form-label">优先级</label>
        <input v-model.number="form.priority" type="number" class="form-input" min="1" />
      </div>
      <div class="form-group" style="flex: 1;">
        <label class="form-label">权重</label>
        <input v-model.number="form.weight" type="number" class="form-input" min="1" />
      </div>
    </div>

    <!-- CPA 特有配置 -->
    <div v-if="form.type === 'cpa'" class="form-group">
      <label class="form-label">CPA 配置</label>
      <div style="background: var(--gray-50); border-radius: 8px; padding: 16px; display: flex; flex-direction: column; gap: 12px;">
        <div>
          <label class="form-label" style="font-size: 12px; margin-bottom: 4px;">启用的 Providers</label>
          <div class="checkbox-group">
            <label v-for="p in cpaProviders" :key="p" class="checkbox-label">
              <input v-model="form.cpa.providers" :value="p" type="checkbox" />
              {{ p }}
            </label>
          </div>
        </div>
        <div>
          <label class="form-label" style="font-size: 12px; margin-bottom: 4px;">账户模式</label>
          <select v-model="form.cpa.account_mode" class="form-select">
            <option value="single">单账户 (single)</option>
            <option value="multi">多账户 (multi)</option>
          </select>
        </div>
        <label class="checkbox-label">
          <input v-model="form.cpa.auto_detect" type="checkbox" />
          自动探测模型和 Provider
        </label>
      </div>
    </div>

    <div class="form-group">
      <label class="form-label">能力声明</label>
      <div class="checkbox-group">
        <label class="checkbox-label">
          <input v-model="form.capabilities.function_calling" type="checkbox" :disabled="form.type === 'cpa'" />
          Function Calling
          <span v-if="form.type === 'cpa'" style="font-size: 11px; color: var(--gray-500);">（按 Provider 自动判断）</span>
        </label>
        <label v-if="form.type !== 'cpa'" class="checkbox-label">
          <input v-model="form.capabilities.extended_thinking" type="checkbox" />
          Extended Thinking
        </label>
        <span v-else style="font-size: 11px; color: var(--gray-500);">CPA 不支持 Extended Thinking</span>
        <label class="checkbox-label">
          <input v-model="form.capabilities.vision" type="checkbox" />
          Vision
        </label>
      </div>
    </div>

    <div class="form-group">
      <label class="form-label">支持的模型（每行一个）</label>
      <textarea
        v-model="modelsText"
        class="form-input"
        rows="3"
        placeholder="gpt-4&#10;gpt-3.5-turbo&#10;claude-3-opus"
      ></textarea>
    </div>

    <div class="form-group">
      <label class="checkbox-label">
        <input v-model="form.enabled" type="checkbox" />
        启用此源
      </label>
    </div>

    <div class="modal-footer" style="padding: 0; border: none; margin-top: 24px;">
      <button type="button" class="btn btn-secondary" @click="$emit('cancel')">取消</button>
      <button type="submit" class="btn btn-primary">{{ source ? '保存' : '添加' }}</button>
    </div>
  </form>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import type { Source } from '../api'

const props = defineProps<{
  source?: Source | null
}>()

const emit = defineEmits<{
  submit: [data: Partial<Source>]
  cancel: []
}>()

const cpaProviders = ['gemini', 'claude', 'codex', 'qwen']

const form = ref({
  name: '',
  type: 'newapi' as Source['type'],
  base_url: '',
  api_key: '',
  priority: 1,
  weight: 100,
  enabled: true,
  capabilities: {
    function_calling: true,
    extended_thinking: false,
    vision: false,
    models: [] as string[]
  },
  cpa: {
    providers: [] as string[],
    account_mode: 'single',
    auto_detect: true
  }
})

const modelsText = computed({
  get: () => form.value.capabilities.models.join('\n'),
  set: (val: string) => {
    form.value.capabilities.models = val.split('\n').map(s => s.trim()).filter(Boolean)
  }
})

watch(() => props.source, (source) => {
  if (source) {
    form.value = {
      name: source.name,
      type: source.type,
      base_url: source.base_url,
      api_key: '',
      priority: source.priority,
      weight: source.weight,
      enabled: source.enabled,
      capabilities: { ...source.capabilities },
      cpa: source.cpa ? { ...source.cpa, providers: [...source.cpa.providers] } : {
        providers: [],
        account_mode: 'single',
        auto_detect: true
      }
    }
  } else {
    form.value = {
      name: '',
      type: 'newapi',
      base_url: '',
      api_key: '',
      priority: 1,
      weight: 100,
      enabled: true,
      capabilities: {
        function_calling: true,
        extended_thinking: false,
        vision: false,
        models: []
      },
      cpa: {
        providers: [],
        account_mode: 'single',
        auto_detect: true
      }
    }
  }
}, { immediate: true })

// CPA 类型切换时自动调整
watch(() => form.value.type, (type) => {
  if (type === 'cpa') {
    form.value.capabilities.extended_thinking = false
  }
})

function handleSubmit() {
  const data: Partial<Source> = {
    name: form.value.name,
    type: form.value.type,
    base_url: form.value.base_url,
    priority: form.value.priority,
    weight: form.value.weight,
    enabled: form.value.enabled,
    capabilities: form.value.capabilities
  }

  if (form.value.api_key) {
    data.api_key = form.value.api_key
  }

  // CPA 类型附加配置
  if (form.value.type === 'cpa') {
    data.cpa = form.value.cpa
  }

  emit('submit', data)
}
</script>
