<script setup lang="ts">
// 自测面板（v0.23 单组；2026-10-29 多组升级）：多组自定义输入直跑沙箱，可选期望
// 输出做逐行比对。「添加自测」加组、「一键填充样例」由父组件传入解析好的题面样例
// （parseIOSamples）、「运行所有自测」串行跑全部组（服务端每人 10 次/分钟限流，
// 命中即停、已出结果保留）。不走判题队列、不留提交记录；在线编程与挑战页共用。
import { ref } from 'vue'
import { ElMessage } from 'element-plus'

import { problemApi } from '@/api'
import type { SelfTestResult } from '@/types'

const props = defineProps<{
  problemId: number
  language: string
  code: string
  samples?: { stdin: string; expected: string }[]
}>()

interface SelfGroup {
  stdin: string
  expected: string
  result: SelfTestResult | null
  running: boolean
}

const mkGroup = (): SelfGroup => ({ stdin: '', expected: '', result: null, running: false })
const groups = ref<SelfGroup[]>([mkGroup()])
const runningAll = ref(false)

function addGroup() {
  groups.value.push(mkGroup())
}

function removeGroup(i: number) {
  groups.value.splice(i, 1)
}

// 一键填充样例：空组原地填、其余追加；stdin 与现有组重复的样例跳过（防重复点击堆积）
function fillSamples() {
  const list = props.samples ?? []
  if (!list.length) return
  const norm = (s: string) => s.replace(/\r\n/g, '\n').trim()
  let filled = 0
  for (const s of list) {
    if (groups.value.some((g) => norm(g.stdin) === norm(s.stdin))) continue
    const empty = groups.value.find((g) => !g.stdin && !g.expected && !g.result)
    if (empty) {
      empty.stdin = s.stdin
      empty.expected = s.expected
    } else {
      groups.value.push({ ...mkGroup(), stdin: s.stdin, expected: s.expected })
    }
    ++filled
  }
  if (filled) ElMessage.success(`已填充 ${filled} 组样例`)
  else ElMessage.info('样例已全部在自测中')
}

// 与期望输出的比对：整体归一（行尾空白/文末换行无关）；无期望输出时不判定
const norm = (s: string) =>
  s
    .replace(/\r\n/g, '\n')
    .split('\n')
    .map((l) => l.trimEnd())
    .join('\n')
    .trim()

function passOf(g: SelfGroup): boolean | null {
  if (!g.result || !g.expected.trim()) return null
  return norm(g.result.stdout) === norm(g.expected) && g.result.status === 'Accepted'
}

async function runAll() {
  if (runningAll.value) return
  if (!props.code.trim()) {
    ElMessage.warning('先写下你的代码再自测')
    return
  }
  if (!groups.value.length) groups.value.push(mkGroup())
  runningAll.value = true
  try {
    // 串行跑：结果逐组出现，进度可感知；服务端限流命中（如 10 次/分钟）即停
    for (const g of groups.value) {
      g.running = true
      g.result = null
      try {
        g.result = await problemApi.selfTest(props.problemId, props.language, props.code, g.stdin)
      } catch {
        return // 拦截器已 toast（限流/服务错误）；已出的结果保留
      } finally {
        g.running = false
      }
    }
  } finally {
    runningAll.value = false
  }
}
</script>

<template>
  <div class="stest">
    <div v-for="(g, i) in groups" :key="i" class="stest-group">
      <div class="stest-ghead">
        <span class="stest-gtitle">自测 #{{ i + 1 }}</span>
        <span v-if="g.running" class="stest-meta">运行中…</span>
        <span v-else-if="g.result" class="stest-meta">
          {{ g.result.status }} · {{ g.result.time_ms }}ms · {{ g.result.mem_kb }}KB
          <template v-if="g.result.truncated">（输出过长已截断）</template>
          <template v-if="passOf(g) !== null">
            ·
            <b :class="passOf(g) ? 'pass' : 'fail'">{{
              passOf(g) ? '与期望输出一致 ✓' : '与期望输出不一致 ✗'
            }}</b>
          </template>
        </span>
        <el-button
          class="stest-del"
          link
          type="danger"
          size="small"
          :disabled="runningAll"
          @click="removeGroup(i)"
        >
          删除
        </el-button>
      </div>
      <div class="stest-io">
        <div class="stest-block">
          <div class="stest-label">测试输入（stdin）</div>
          <el-input
            v-model="g.stdin"
            type="textarea"
            :rows="3"
            placeholder="程序运行时读取的输入"
            resize="none"
          />
        </div>
        <div class="stest-block">
          <div class="stest-label">期望输出（可选）</div>
          <el-input
            v-model="g.expected"
            type="textarea"
            :rows="3"
            placeholder="留空只展示运行结果"
            resize="none"
          />
        </div>
      </div>
      <pre v-if="g.result?.stdout" class="stest-out">{{ g.result.stdout }}</pre>
      <pre v-if="g.result?.stderr" class="stest-err">{{ g.result.stderr }}</pre>
    </div>
    <div class="stest-act">
      <el-button size="small" :disabled="runningAll" @click="addGroup">添加自测</el-button>
      <div class="stest-act-right">
        <el-button
          v-if="samples?.length"
          size="small"
          :disabled="runningAll"
          @click="fillSamples"
        >
          一键填充样例
        </el-button>
        <el-button type="primary" size="small" :loading="runningAll" @click="runAll">
          运行所有自测
        </el-button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.stest {
  padding: 10px 12px;
  border-radius: 12px;
  background: #fff;
  border: 1px solid var(--soj-line, #e6eaf2);
}
.stest-group + .stest-group {
  margin-top: 10px;
  padding-top: 10px;
  border-top: 1px dashed var(--soj-line, #e6eaf2);
}
.stest-ghead {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 4px;
  min-height: 24px;
}
.stest-gtitle {
  font-size: 12px;
  font-weight: 700;
  color: var(--soj-ink-2, #475569);
}
.stest-del {
  margin-left: auto;
}
.stest-io {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
}
.stest-block {
  min-width: 0;
}
.stest-label {
  font-size: 12px;
  font-weight: 700;
  color: var(--soj-ink-3, #64748b);
  margin-bottom: 4px;
}
.stest-act {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 10px;
}
.stest-act-right {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 10px;
}
.stest-meta {
  font-size: 12.5px;
  color: var(--soj-ink-3, #64748b);
}
.stest-meta .pass {
  color: var(--soj-success, #3cb371);
}
.stest-meta .fail {
  color: var(--soj-danger, #ff6b6b);
}
.stest-out,
.stest-err {
  margin: 8px 0 0;
  padding: 8px 10px;
  max-height: 140px;
  overflow: auto;
  border-radius: 8px;
  background: #f6f8fb;
  font-family: JetBrains Mono, Consolas, monospace;
  font-size: 12.5px;
  white-space: pre-wrap;
  word-break: break-all;
}
.stest-err {
  background: #fff4f4;
  color: #b4443c;
}
</style>
