<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'

import { useTagTreeStore } from '@/stores/tagtree'

// 表格标签列：单元格只显示一级标签（量宽截断，能放下几个显示几个）；
// 只要有放不下的（或没有任何一级标签），悬停整格 50ms 用同款彩色样式展示全部标签。
const props = defineProps<{ ids: number[] }>()
const tagStore = useTagTreeStore()

const lv1 = computed(() => props.ids.filter((id) => tagStore.levelOf(id) === 1))

const box = ref<HTMLElement>()
const visible = ref(props.ids.length)
let ro: ResizeObserver | undefined

const overflow = computed(() => visible.value < lv1.value.length)
// 未显示的标签数：一级标签被裁掉的 + 不上屏的子级标签
const hidden = computed(() => props.ids.length - visible.value)
// 一级标签放不下、或存在任何未显示的标签（含子级）时，悬停展示全量
const showPop = computed(
  () => props.ids.length > 0 && (overflow.value || lv1.value.length < props.ids.length),
)

function measure() {
  const el = box.value
  if (!el) return
  const avail = el.clientWidth
  if (!avail) return
  const tags = Array.from(el.querySelectorAll<HTMLElement>('.fit-tag'))
  const GAP = 6
  const fitCount = (reserve: number) => {
    let acc = 0
    let count = 0
    for (let i = 0; i < tags.length; i++) {
      acc += tags[i].offsetWidth + (i > 0 ? GAP : 0)
      if (acc + reserve > avail) break
      count = i + 1
    }
    return count
  }
  let count = fitCount(0)
  if (count < tags.length) {
    // 有溢出：预留行尾 +N 徽标的真实宽度再算一遍（徽标可能未渲染，先用估算值）
    const badge = el.querySelector<HTMLElement>('.fit-more')
    count = fitCount((badge?.offsetWidth ?? 30) + GAP)
  }
  visible.value = count
}

function schedule() {
  visible.value = lv1.value.length
  nextTick(measure)
}

watch(() => [props.ids, lv1.value.length], schedule)

onMounted(() => {
  ro = new ResizeObserver(() => measure())
  if (box.value) ro.observe(box.value)
  document.fonts?.ready.then(() => measure())
  measure()
})

onBeforeUnmount(() => ro?.disconnect())
</script>

<template>
  <!-- tooltip 包住整格：标签栏任一处悬停 50ms 展示全部标签（含子级），同表格五级色样式 -->
  <el-tooltip :show-after="50" :disabled="!showPop" placement="top" effect="light">
    <template #content>
      <div class="fit-pop">
        <el-tag
          v-for="id in ids"
          :key="id"
          size="small"
          class="fit-tag"
          :class="`lv-${tagStore.levelOf(id)}`"
        >
          {{ tagStore.nameOf(id) }}
        </el-tag>
      </div>
    </template>
    <div ref="box" class="fit-tags">
      <template v-if="lv1.length">
        <el-tag
          v-for="id in lv1"
          :key="id"
          size="small"
          class="fit-tag"
          :class="`lv-${tagStore.levelOf(id)}`"
        >
          {{ tagStore.nameOf(id) }}
        </el-tag>
        <el-tag v-if="hidden > 0" size="small" type="info" class="fit-more">+{{ hidden }}</el-tag>
      </template>
      <span v-else class="dim">—</span>
    </div>
  </el-tooltip>
</template>

<style scoped>
.fit-tags {
  position: relative;
  display: flex;
  flex-wrap: nowrap;
  align-items: center;
  gap: 6px;
  overflow: hidden;
  min-width: 0;
  max-width: 100%;
}

.fit-tag {
  flex: 0 0 auto;
  background: var(--lv-bg);
  border-color: var(--lv-border);
  color: var(--el-text-color-primary);
}

.fit-tag:hover {
  border-color: var(--lv);
}

/* 行尾徽标：绝对定位贴右缘，实底遮住被裁切的残 tag */
.fit-more {
  position: absolute;
  right: 0;
  top: 50%;
  transform: translateY(-50%);
  background: var(--el-bg-color);
  flex: 0 0 auto;
}

/* 悬停弹层：与表格同款彩色标签（teleport 到 body，但模板元素带 scoped 属性，样式生效） */
.fit-pop {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  max-width: 360px;
}
</style>
