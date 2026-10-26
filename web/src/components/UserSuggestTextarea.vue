<script setup lang="ts">
// 用户名联想批量输入（v0.18.3 随题单批次）：批量导入成员的文本域增强——
// 按空格/逗号/换行分隔输入，光标所在最后一段自动调 /users/lookup 联想已注册用户，
// 点击建议或 Enter 选中即回填该段；↑↓ 切换候选，Esc 关闭。纯前端回填，导入逻辑不变。
// 2026-10-26：弹层不限高全量铺开（服务端上限 20）；改 position:fixed 跟手重定位——
// 绝对定位会向下撑出文档滚动区（页面右侧多一根滚动条），fixed 脱离文档流根治；
// 下方视口放不下且上方放得下时向上翻，开着的滚动/缩放实时跟位。
import { nextTick, onBeforeUnmount, ref } from 'vue'

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
const root = ref<HTMLElement>()
const open = ref(false)
const loading = ref(false)
const items = ref<LookupUser[]>([])
const active = ref(-1)
const popStyle = ref<Record<string, string>>({})
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
    items.value = r // 全量列出（服务端上限 20），不截断不内滚
    active.value = -1
    if (items.value.length > 0) openPop()
    else close()
  } finally {
    if (mySeq === seq) loading.value = false
  }
}

// 弹层按实际条目高度定 top：下方放不下且上方放得下时向上翻
function place() {
  const el = root.value
  if (!el || !open.value) return
  const r = el.getBoundingClientRect()
  const h = items.value.length * 34 + 10
  const flip = window.innerHeight - r.bottom < h + 8 && r.top > h + 8
  const top = flip ? r.top - h - 4 : r.bottom + 4
  popStyle.value = {
    left: `${Math.round(r.left)}px`,
    top: `${Math.round(top)}px`,
    width: `${Math.round(r.width)}px`,
  }
}

function onReposition() {
  if (open.value) place()
}

function openPop() {
  open.value = true
  place() // 先按估算占位
  nextTick(place) // 渲染后再校一次
  window.addEventListener('scroll', onReposition, true)
  window.addEventListener('resize', onReposition)
}

function close() {
  open.value = false
  items.value = []
  active.value = -1
  window.removeEventListener('scroll', onReposition, true)
  window.removeEventListener('resize', onReposition)
}

function pick(u: LookupUser) {
  const { start } = lastTokenOf(props.modelValue)
  emit('update:modelValue', `${props.modelValue.slice(0, start)}${u.username} `)
  close()
  input.value?.focus()
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

onBeforeUnmount(close)
</script>

<template>
  <div ref="root" class="usuggest">
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
    <div v-if="open" class="usuggest-pop" :style="popStyle">
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

/* fixed 脱离文档流：坐标由 place() 按文本域视口位置下发（left/top/width 内联），不撑页面滚动 */
.usuggest-pop {
  position: fixed;
  z-index: 20;
  background: #fff;
  border: 1px solid var(--soj-line, #e3eaf6);
  border-radius: 10px;
  box-shadow: 0 10px 28px rgba(11, 31, 69, 0.14);
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
