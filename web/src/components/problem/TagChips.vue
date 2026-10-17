<script setup lang="ts">
// 标签筛选的递归层（v0.5f）：每个标签一枚 chip，有下级时悬停弹出下一层
//（任意深度递归）；悬停只预览不算选择，点击才切换选中。
import type { TagNode } from '@/types'

defineProps<{
  nodes: TagNode[]
  selected: Set<number>
}>()

const emit = defineEmits<{ toggle: [id: number] }>()
</script>

<template>
  <el-popover
    v-for="n in nodes"
    :key="n.id"
    placement="bottom-start"
    trigger="hover"
    :show-after="120"
    :hide-after="60"
    :disabled="!n.children?.length"
    popper-class="tag-chips-popper"
  >
    <template #reference>
      <button
        type="button"
        class="tag-chip"
        :class="{ active: selected.has(n.id) }"
        @click="emit('toggle', n.id)"
      >
        {{ n.name }}
        <span v-if="n.children?.length" class="caret">▸</span>
      </button>
    </template>
    <div class="chips-flow">
      <TagChips :nodes="n.children!" :selected="selected" @toggle="emit('toggle', $event)" />
    </div>
  </el-popover>
</template>

<style scoped>
.tag-chip {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  padding: 2px 10px;
  border: 1px solid var(--el-border-color);
  border-radius: 4px;
  background: var(--el-bg-color);
  color: var(--el-text-color-regular);
  font-size: 15px;
  line-height: 22px;
  cursor: pointer;
  transition: all 0.15s;
}
.tag-chip:hover {
  color: var(--el-color-primary);
  border-color: var(--el-color-primary-light-5);
}
.tag-chip.active {
  color: var(--el-color-white);
  background: var(--el-color-primary);
  border-color: var(--el-color-primary);
}
.caret {
  font-size: 12px;
  opacity: 0.6;
}
.chips-flow {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  max-width: 420px;
}
</style>

<style>
/* 弹层内容脱离组件作用域，全局类收敛命名 */
.tag-chips-popper .chips-flow {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  max-width: 420px;
}
</style>
