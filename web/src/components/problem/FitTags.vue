<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'

import { registerSink, scheduleMeasure, unregisterSink } from '@/utils/fitMeasure'
import { useMetaTreeStore } from '@/stores/metaTree'
import { useTagTreeStore } from '@/stores/tagtree'

// 表格标签列：标签按层级优先级展示（一级 > 二级 > 三级…，同级保持原序），
// 量宽截断——能整颗放下几颗显示几颗，放不下的整颗隐藏（visibility，不允许半裁残字），
// 行尾 +N 汇总；悬停整格 50ms 用同款样式展示全部标签。
// tree（2026-10-25 双树）：knowledge = 知识树（默认），meta = 来源标签树——题表
// 「知识点/标签」列头点击切换时同一列复用本组件，两树 API 同构。
//
// v0.62 挂载冻结根治批：测量改全局单波批调度（utils/fitMeasure——读写分离 + 共享
// ResizeObserver + fonts.ready 全站一波），替代每实例各自 measure/RO/二波测量，
// 消掉 50 行规模挂载期的多轮交错强制布局；tooltip 改「仅溢出行挂载」——展示态零
// el-tooltip 实例（每实例 ~1ms 脚手架 × 行数是题库刷新卡的实测成分之一），溢出行
// 悬停弹层体验不变。
const props = withDefaults(
  defineProps<{ ids: number[]; tree?: 'knowledge' | 'meta' }>(),
  { tree: 'knowledge' },
)
const tagStore = computed(() => (props.tree === 'meta' ? useMetaTreeStore() : useTagTreeStore()))
watch(() => props.tree, () => tagStore.value.ensure(), { immediate: true })

const box = ref<HTMLElement>()
const visible = ref(props.ids.length)
let nextVisible = props.ids.length

// 层级优先级排序（levelOf 未入树标签默认一级）；ES2019+ sort 稳定，同级保序
const sortedIds = computed(() => [...props.ids].sort((a, b) => tagStore.value.levelOf(a) - tagStore.value.levelOf(b)))

const overflow = computed(() => visible.value < props.ids.length)
const hidden = computed(() => props.ids.length - visible.value)

// 量宽只读阶段：算出能放几颗存入 nextVisible，不碰响应式（批调度统一先读后写）
function measureRead() {
  const el = box.value
  if (!el) return
  const avail = el.clientWidth
  if (!avail) {
    nextVisible = props.ids.length // 不可见（display:none）：全显占位，可见后共享 RO 会再测
    return
  }
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
  nextVisible = count
}
function measureWrite() {
  if (visible.value !== nextVisible) visible.value = nextVisible
}

const sink = { read: measureRead, write: measureWrite }
let curEl: HTMLElement | null = null
function bind() {
  if (curEl) unregisterSink(curEl, sink)
  curEl = box.value ?? null
  if (curEl) registerSink(curEl, sink)
}

onMounted(bind)
// overflow 翻转会让 tooltip/纯 box 两分支互切重建元素：重新绑定共享 RO 观测新元素
watch(overflow, () => void nextTick(bind))
onBeforeUnmount(() => {
  if (curEl) unregisterSink(curEl, sink)
})

watch(() => props.ids, () => {
  visible.value = props.ids.length
  scheduleMeasure(sink)
})
</script>

<template>
  <!-- 悬停弹层：仅溢出行挂载 el-tooltip（v0.62——展示态零 tooltip 实例）；两分支内部
       结构需同步改 -->
  <el-tooltip v-if="overflow" :show-after="50" placement="top" effect="light">
    <template #content>
      <div class="fit-pop">
        <el-tag
          v-for="id in sortedIds"
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
      <template v-if="sortedIds.length">
        <el-tag
          v-for="(id, i) in sortedIds"
          :key="id"
          size="small"
          class="fit-tag"
          :class="[`lv-${tagStore.levelOf(id)}`, { 'fit-hide': i >= visible }]"
        >
          {{ tagStore.nameOf(id) }}
        </el-tag>
        <el-tag v-if="hidden > 0" size="small" type="info" class="fit-more">+{{ hidden }}</el-tag>
      </template>
      <span v-else class="dim">—</span>
    </div>
  </el-tooltip>
  <div v-else ref="box" class="fit-tags">
    <template v-if="sortedIds.length">
      <el-tag
        v-for="(id, i) in sortedIds"
        :key="id"
        size="small"
        class="fit-tag"
        :class="[`lv-${tagStore.levelOf(id)}`, { 'fit-hide': i >= visible }]"
      >
        {{ tagStore.nameOf(id) }}
      </el-tag>
      <el-tag v-if="hidden > 0" size="small" type="info" class="fit-more">+{{ hidden }}</el-tag>
    </template>
    <span v-else class="dim">—</span>
  </div>
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
  color: var(--lv);
}

.fit-tag:hover {
  border-color: var(--lv);
}

/* 落选标签整颗隐身（占位不显示）：任何标签都不允许被裁半个字 */
.fit-hide {
  visibility: hidden;
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

/* 悬停弹层：与表格同款层级色标签（teleport 到 body，但模板元素带 scoped 属性，样式生效） */
.fit-pop {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  max-width: 360px;
}
</style>
