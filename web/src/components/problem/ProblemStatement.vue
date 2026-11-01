<script setup lang="ts">
import { computed } from 'vue'
// 题面（v3-full 重制）：不再套白卡、直接立于画布——面包屑上方由父视图渲染；
// 结构 = kicker PROBLEM + 右上 96px 描边空心题号 + 32/800 标题（pid 胶囊 + 标题 +
// 题型 + 难度）+ prob-meta 分格条（时限/内存/通过数/标签）+ 提示区 + 题面正文。
// 详情页主列、在线编程左栏共用；标题行右侧扩展位走 #extra 插槽（详情页放隐藏开关）。
import MdView from '@/components/MdView.vue'
import TierBadge from '@/components/TierBadge.vue'
import { useMetaTreeStore } from '@/stores/metaTree'
import { useTagTreeStore } from '@/stores/tagtree'
import { fmtMem, fmtSec } from '@/utils/format'
import type { ProblemDetail } from '@/types'

const props = defineProps<{
  problem: ProblemDetail
  fileIo?: boolean // 比赛/考试启用文件 IO 时显示提示（做题页主列与在线编程左栏同款）
}>()

const tagStore = useTagTreeStore()
tagStore.ensure()

const metaStore = useMetaTreeStore()
metaStore.ensure()

// 客观题（v0.9.3）：整卷题型摘要徽章（全同型给具体类型，混合给「客观题」）
const isObjective = computed(() => props.problem.type === 'objective')
const objectiveLabel = computed(() => {
  const qs = props.problem.questions ?? []
  if (!qs.length) return '客观题'
  const kinds = new Set(qs.map((q) => q.kind))
  if (kinds.size > 1) return `客观题 · ${qs.length} 题`
  const map: Record<string, string> = { single: '单选题', multiple: '多选题', judge: '判断题', blank: '填空题' }
  return map[qs[0].kind] ?? '客观题'
})

// 函数题/交互题徽章（v0.9.1/v0.9.2）
const kindLabel = computed(() => {
  if (props.problem.type === 'objective') return objectiveLabel.value
  if (props.problem.type === 'function') return '函数题'
  if (props.problem.type === 'interactive') return '交互题'
  return ''
})

// 知识点（prob-meta 元信息条格）：格内只露「知识点」label 保一行，悬停立即浮层
// 平铺全量 chips（2026-11-01：原格内平铺/展开随名字变长挤成两行，撤）；chip 挂
// 五级层级色与题库列表 FitTags 统一
const metaTags = computed(() =>
  (props.problem.tags ?? []).map((id) => ({ id, name: tagStore.nameOf(id), lv: tagStore.levelOf(id) })),
)

// 标签（标签树：地区/赛事/题库/年份，格内同知识点收敛、悬停浮层平铺；
// 2026-11-01「来源」更名「标签」）
const labelTags = computed(() =>
  (props.problem.meta_tags ?? []).map((id) => ({ id, name: metaStore.nameOf(id) })),
)

// 空心大题号取后四位（P1024 → 1024；三位以内原样）
const bigNo = computed(() => String(props.problem.id))
</script>

<template>
  <div class="statement">
    <div class="prob-head">
      <span class="soj-big-no">{{ bigNo }}</span>
      <div class="kicker">PROBLEM</div>
      <h1 class="prob-title">
        <span class="pid mono">P{{ problem.id }}</span>
        <span class="t">{{ problem.title }}</span>
        <el-tag v-if="kindLabel" size="small" class="kind-tag ptype-tag">{{ kindLabel }}</el-tag>
        <span class="extra"><slot name="extra" /></span>
      </h1>
    </div>

    <!-- v3 prob-meta 分格条：时限/内存/通过数/难度/知识点/标签（难度在知识点左）。
         知识点/标签格只露 label 保整条一行不换行，悬停立即浮层平铺全量 chips
         （2026-11-01：原格内平铺/展开随名字变长把分格条挤成两行，撤） -->
    <div class="soj-prob-meta">
      <template v-if="!isObjective">
        <div class="cell">时间限制 <b class="mono">{{ fmtSec(problem.time_limit_ms) }}</b></div>
        <div class="cell">内存限制 <b class="mono">{{ fmtMem(problem.memory_limit_kb) }}</b></div>
        <div class="cell">
          <b class="mono">{{ problem.accepted_count ?? 0 }}</b> 通过 ／
          <b class="mono">{{ problem.submit_count ?? 0 }}</b> 提交
        </div>
      </template>
      <div v-else class="cell">客观题 · <b>{{ problem.questions?.length ?? 0 }}</b> 小题</div>
      <!-- 赛时隐藏难度（hideMeta）时服务端不下发 difficulty，整格隐藏而非按 D3 兜底 -->
      <div v-if="problem.difficulty != null" class="cell diff-cell">
        <TierBadge :tier="problem.difficulty" kind="difficulty" />
      </div>
      <div v-if="metaTags.length" class="cell">
        <el-tooltip placement="top" effect="light">
          <template #content>
            <div class="tag-pop">
              <span
                v-for="t in metaTags"
                :key="t.id"
                class="soj-tagchip meta-chip"
                :class="`lv-${t.lv}`"
              >{{ t.name }}</span>
            </div>
          </template>
          <span class="tag-brief">知识点</span>
        </el-tooltip>
      </div>
      <div v-if="labelTags.length" class="cell">
        <el-tooltip placement="top" effect="light">
          <template #content>
            <div class="tag-pop">
              <span v-for="t in labelTags" :key="t.id" class="soj-tagchip meta-chip src-chip">{{ t.name }}</span>
            </div>
          </template>
          <span class="tag-brief">标签</span>
        </el-tooltip>
      </div>
    </div>

    <el-alert
      v-if="fileIo"
      type="warning"
      :closable="false"
      class="fileio-alert notice-alert"
      show-icon
      title="本题请以文件 IO 方式提交"
      :description="`输入输出文件名是 ${problem.file_io_input || 'problem.in'} / ${problem.file_io_output || 'problem.out'}`"
    />
    <!-- 交互题（v0.9.2）：交互方式说明（选手不读输入文件，通过 stdin/stdout 与交互器通信）；
         ptype-alert = 题型徽章同族浅青（main.css） -->
    <el-alert
      v-if="problem.type === 'interactive'"
      type="info"
      :closable="false"
      class="fileio-alert ptype-alert"
      show-icon
      title="本题为交互题：通过标准输入输出与评测器交互"
      description="评测器会实时读取你的输出并回应，按题面约定的协议交替读写；不要使用文件读写。仅支持 C / C++。"
    />
    <!-- 无测试数据空壳题（v0.6.1 验收反馈）：提示样式与文件 IO 提示同款，分角色文案 -->
    <el-alert
      v-if="!isObjective && problem.has_cases === false"
      type="warning"
      :closable="false"
      class="fileio-alert notice-alert"
      show-icon
      title="本题暂无测试数据，不可提交"
    />
    <MdView :content="problem.description" :html="problem.description_html" />
  </div>
</template>

<style scoped>
/* v3 .prob-head：kicker + 大标题 + 右上空心题号（绝对定位锚点） */
.prob-head {
  position: relative;
  padding: 6px 0 18px;
}

.prob-title {
  margin: 0;
  font-size: var(--soj-fs-prob-title);
  font-weight: var(--soj-fw-heavy);
  letter-spacing: -0.6px;
  line-height: 1.3;
  color: var(--soj-navy);
  display: flex;
  align-items: center;
  gap: 14px;
  flex-wrap: wrap;
}

/* v3 .pid：mono 主蓝浅底胶囊 */
.pid {
  flex: 0 0 auto;
  font-size: var(--soj-fs-16);
  font-weight: var(--soj-fw-emphasis);
  color: var(--soj-blue);
  background: var(--soj-blue-soft);
  padding: 4px 12px;
  border-radius: 10px;
}

.prob-title .t {
  min-width: 0;
  overflow-wrap: anywhere;
}

.kind-tag {
  flex: 0 0 auto;
}

/* 右侧扩展位（隐藏开关等）：跟随标题行内排（2026-10-25 从行尾回调到题旁） */
.extra {
  margin-left: 10px;
  display: inline-flex;
  align-items: center;
}

/* 窄屏收起空心题号（v3 640px 断点同款） */
@media (max-width: 640px) {
  .soj-big-no {
    display: none;
  }
}

.fileio-alert {
  margin-top: 16px;
}

.meta-chip {
  height: 20px;
  cursor: default;
  flex: none;
  /* 五级层级色（--lv 由全局 .lv-N 注入）：与题库列表 FitTags 统一（修复⑰） */
  color: var(--lv);
  background: var(--lv-bg);
  border-color: var(--lv-border);
}

/* 悬停浮层（el-tooltip light 内容）：全量 chips 平铺，样式同格内原展开态；
   格内只露 label 不占宽，浮层随内容自适应换行（2026-11-01） */
.tag-pop {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  max-width: 360px;
}

/* 浮层 chip 提到 13px 档（全局 soj-tagchip 12px 偏小，2026-11-01 反馈；只提浮层，
   题库列表等可点击筛选 chip 不动） */
.tag-pop .meta-chip {
  font-size: var(--soj-fs-13);
}

/* 标签 chips：单色中性（石墨蓝），与知识树五级色区分（2026-10-25 双树） */
.src-chip {
  color: var(--soj-ink-2);
  background: var(--soj-surface-2);
  border-color: rgba(11, 31, 69, 0.13);
}
</style>
