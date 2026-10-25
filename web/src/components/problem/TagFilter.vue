<script setup lang="ts">
// 标签多选卡片流（题库筛选 + 建题/编辑表单共用）：悬停展开下级、可搜索、点击选中；
// 多选并集、精确匹配，选中集变化即上抛。已选展示在标签搜索框右边（TagChips 顶行
// 插槽），showSelected=false 可关闭（外部自带已选展示时用）。
// tree（2026-10-25 双树）：knowledge = 知识树（默认），meta = 来源标签树（地区/赛事/题库/年份）。
import { computed, onMounted } from 'vue'

import { useMetaTreeStore } from '@/stores/metaTree'
import { useTagTreeStore } from '@/stores/tagtree'
import TagChips from './TagChips.vue'

const props = withDefaults(
  defineProps<{ selected: number[]; showSelected?: boolean; tree?: 'knowledge' | 'meta' }>(),
  { showSelected: true, tree: 'knowledge' },
)
const emit = defineEmits<{ change: [ids: number[]] }>()

const tagStore = props.tree === 'meta' ? useMetaTreeStore() : useTagTreeStore()

const selectedSet = computed(() => new Set(props.selected))

function toggle(id: number) {
  emit('change', props.selected.includes(id) ? props.selected.filter((x) => x !== id) : [...props.selected, id])
}

onMounted(() => tagStore.ensure())
</script>

<template>
  <div class="tag-filter">
    <TagChips
      :nodes="tagStore.nodes"
      :selected="selectedSet"
      :placeholder="props.tree === 'meta' ? '搜索标签…' : '搜索知识点…'"
      @toggle="toggle"
    >
      <template #top-right>
        <template v-if="showSelected && selected.length">
          <span class="dim">已选：</span>
          <el-tag
            v-for="id in selected"
            :key="id"
            size="small"
            closable
            :class="`lv-${tagStore.levelOf(id)}`"
            :style="{ color: 'var(--lv)', background: 'var(--lv-bg)', borderColor: 'var(--lv-border)' }"
            @close="toggle(id)"
          >
            {{ tagStore.nameOf(id) }}
          </el-tag>
          <el-button link type="primary" size="small" @click="emit('change', [])">清空</el-button>
        </template>
      </template>
    </TagChips>
  </div>
</template>

<style scoped>
.tag-filter {
  flex: 1;
  min-width: 0;
}
</style>
