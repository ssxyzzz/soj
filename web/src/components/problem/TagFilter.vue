<script setup lang="ts">
// 标签多选卡片流（题库筛选 + 建题/编辑表单共用）：悬停展开下级、可搜索、点击选中；
// 多选并集、精确匹配，选中集变化即上抛。已选行常显便于取消。
import { computed, onMounted } from 'vue'

import { useTagTreeStore } from '@/stores/tagtree'
import TagChips from './TagChips.vue'

const props = defineProps<{ selected: number[] }>()
const emit = defineEmits<{ change: [ids: number[]] }>()

const tagStore = useTagTreeStore()

const selectedSet = computed(() => new Set(props.selected))

function toggle(id: number) {
  emit('change', props.selected.includes(id) ? props.selected.filter((x) => x !== id) : [...props.selected, id])
}

onMounted(() => tagStore.ensure())
</script>

<template>
  <div class="tag-filter">
    <div class="chips-flow">
      <TagChips :nodes="tagStore.nodes" :selected="selectedSet" @toggle="toggle" />
    </div>
    <div v-if="selected.length" class="selected-row">
      <span class="dim">已选：</span>
      <el-tag
        v-for="id in selected"
        :key="id"
        size="small"
        closable
        @close="toggle(id)"
      >
        {{ tagStore.nameOf(id) }}
      </el-tag>
      <el-button link type="primary" size="small" @click="emit('change', [])">清空</el-button>
    </div>
  </div>
</template>

<style scoped>
.tag-filter {
  flex: 1;
  min-width: 0;
}
.chips-flow {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.selected-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 6px;
}
</style>
