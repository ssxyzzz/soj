<script setup lang="ts">
import { computed } from 'vue'
// 题面（v3-full 重制）：不再套白卡、直接立于画布——面包屑上方由父视图渲染；
// 结构 = kicker PROBLEM + 右上 96px 描边空心题号 + 32/800 标题（pid 胶囊 + 标题 +
// 题型 + 难度）+ prob-meta 分格条（时限/内存/通过数/标签）+ 提示区 + 题面正文。
// 详情页主列、在线编程左栏共用；标题行右侧扩展位走 #extra 插槽（详情页放隐藏开关）。
import MdView from '@/components/MdView.vue'
import TierBadge from '@/components/TierBadge.vue'
import { useTagTreeStore } from '@/stores/tagtree'
import { fmtMem, fmtSec } from '@/utils/format'
import type { ProblemDetail } from '@/types'

const props = defineProps<{
  problem: ProblemDetail
  fileIo?: boolean // 比赛/考试启用文件 IO 时显示提示（做题页主列与在线编程左栏同款）
}>()

const tagStore = useTagTreeStore()
tagStore.ensure()

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

// 标签平铺（prob-meta 标签格；最多 4 枚防溢出）——五级层级色与题库列表统一
//（2026-10-25 修复⑰：原 soj-tagchip 青系单色与题库 lv 色不一致）
const metaTags = computed(() =>
  (props.problem.tags ?? []).slice(0, 4).map((id) => ({ id, name: tagStore.nameOf(id), lv: tagStore.levelOf(id) })),
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

    <!-- v3 prob-meta 分格条：时限/内存/通过数/难度/标签（难度在标签左；标签悬停显示，见 .meta-chips） -->
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
      <div class="cell diff-cell">
        <TierBadge :tier="problem.difficulty ?? 3" kind="difficulty" />
      </div>
      <div v-if="metaTags.length" class="cell tag-cell">
        标签
        <span class="meta-chips">
          <span
            v-for="t in metaTags"
            :key="t.id"
            class="soj-tagchip meta-chip"
            :class="`lv-${t.lv}`"
          >{{ t.name }}</span>
        </span>
      </div>
    </div>

    <el-alert
      v-if="fileIo"
      type="warning"
      :closable="false"
      class="fileio-alert"
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
      class="fileio-alert"
      show-icon
      title="本题暂无测试数据，不可提交"
    />
    <MdView :content="problem.description" />
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
  margin-left: 4px;
  cursor: default;
  /* 五级层级色（--lv 由全局 .lv-N 注入）：与题库列表 FitTags 统一（修复⑰） */
  color: var(--lv);
  background: var(--lv-bg);
  border-color: var(--lv-border);
}

/* 标签格：默认只露出「标签」label，chips 透明占位（分格条不跳宽）；
   悬停本格快速淡入，移走即缓慢淡出、1 秒淡尽（2026-10-25 用户回调，原 3s 延迟撤）——
   纯 transition 时长承载，无需 JS 计时器 */
.tag-cell {
  cursor: default;
}

.meta-chips {
  display: inline-flex;
  align-items: center;
  opacity: 0;
  transition: opacity 1s ease 0s;
}

.tag-cell:hover .meta-chips {
  opacity: 1;
  transition: opacity 0.15s ease 0s;
}
</style>
