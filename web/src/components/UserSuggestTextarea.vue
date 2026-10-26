<script setup lang="ts">
// 用户名联想批量输入（v0.18.3 随题单批次）：批量导入成员的文本域增强——
// 按空格/逗号/换行分隔输入，光标所在最后一段自动调 /users/lookup 联想已注册用户，
// 点击建议或 Enter 选中即回填该段；↑↓ 切换候选，Esc 关闭。纯前端回填，导入逻辑不变。
import { ref } from 'vue'

import { userApi } from '@/api'
import type { LookupUser } from '@/types'

const props = withDefaults(
  defineProps<{
    modelValue: string
    placeholder?: string
    rows?: number
  }>(),
  { placeholder: '', rows: 2 },
)
const emit = defineEmits<{ (e: 'update:modelValue', v: string): void }>()

const input = ref()
const open = ref(false)
const loading = ref(false)
const items = ref<LookupUser[]>([])
const active = ref(-1)
let debounce: ReturnType<typeof setTimeout> | undefined
let seq = 0

// 最后一个未完成段（被空格/逗号/换行切开的尾段）：起点下标 + 关键词
function lastTokenOf(v: string): { start: number; kw: string } {
  const m = /[^,,\s]+$/.exec(v)
  const start = m ? m.index : v.length
  return { start, kw: v.slice(start).trim() }
}

function onInput(v: string) {
  emit('update:modelValue', v)
  const { kw } = lastTokenOf(v)
  if (!kw) {
    close()
    return
  }
  clearTimeout(debounce)
  debounce = setTimeout(() => lookup(kw), 250)
}

async function lookup(kw: string) {
  const mySeq = ++seq
  loading.value = true
  try {
    const r = await userApi.lookup(kw)
    if (mySeq !== seq) return // 过期响应丢弃
    items.value = r.slice(0, 8)
    active.value = -1
    open.value = items.value.length > 0
  } finally {
    if (mySeq === seq) loading.value = false
  }
}

function pick(u: LookupUser) {
  const { start } = lastTokenOf(props.modelValue)
  emit('update:modelValue', `${props.modelValue.slice(0, start)}${u.username} `)
  close()
  input.value?.focus()
}

function close() {
  open.value = false
  items.value = []
  active.value = -1
}

// 键盘：候选开着时 ↑↓ 移动 / Enter 选中 / Esc 关闭；否则 Enter 换行照旧
function onKeydown(e: Event) {
  if (!(e instanceof KeyboardEvent)) return
  if (!open.value || !items.value.length) return
  if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
    e.preventDefault()
    const dir = e.key === 'ArrowDown' ? 1 : -1
    active.value = (active.value + dir + items.value.length) % items.value.length
  } else if (e.key === 'Enter') {
    e.preventDefault()
    pick(items.value[Math.max(active.value, 0)])
  } else if (e.key === 'Escape') {
    close()
  }
}
</script>

<template>
  <div class="usuggest">
    <el-input
      ref="input"
      :model-value="modelValue"
      type="textarea"
      :rows="rows"
      :placeholder="placeholder"
      @update:model-value="onInput"
      @keydown="onKeydown"
      @blur="close"
    />
    <div v-if="open" class="usuggest-pop">
      <div
        v-for="(u, i) in items"
        :key="u.id"
        class="usug-item"
        :class="{ on: i === active }"
        @mousedown.prevent="pick(u)"
      >
        <span class="mono">{{ u.username }}</span>
        <span v-if="u.nickname" class="nick">{{ u.nickname }}</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.usuggest {
  position: relative;
  width: 100%;
}

.usuggest-pop {
  position: absolute;
  top: calc(100% + 4px);
  left: 0;
  right: 0;
  z-index: 20;
  background: #fff;
  border: 1px solid var(--soj-line, #e3eaf6);
  border-radius: 10px;
  box-shadow: 0 10px 28px rgba(11, 31, 69, 0.14);
  max-height: 240px;
  overflow-y: auto;
  padding: 4px;
}

.usug-item {
  display: flex;
  align-items: baseline;
  gap: 8px;
  padding: 7px 10px;
  border-radius: 7px;
  cursor: pointer;
  font-size: 13.5px;
}

.usug-item.on,
.usug-item:hover {
  background: #f0f5fc;
}

.usug-item .nick {
  color: var(--soj-ink-3, #5b6b85);
  font-size: 12.5px;
}
</style>
