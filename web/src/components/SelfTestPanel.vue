<script setup lang="ts">
// 自测面板（v0.23）：自定义输入直跑沙箱单测，可选期望输出做逐行比对。
// 不走判题队列、不留提交记录；在线编程与挑战页共用（挑战中可用且计时照走）。
import { computed, ref } from 'vue'
import { ElMessage } from 'element-plus'

import { problemApi } from '@/api'
import type { SelfTestResult } from '@/types'

const props = defineProps<{
  problemId: number
  language: string
  code: string
}>()

const input = ref('')
const expected = ref('')
const running = ref(false)
const result = ref<SelfTestResult | null>(null)

async function run() {
  if (running.value) return
  if (!props.code.trim()) {
    ElMessage.warning('先写下你的代码再自测')
    return
  }
  running.value = true
  result.value = null
  try {
    result.value = await problemApi.selfTest(props.problemId, props.language, props.code, input.value)
  } catch {
    /* 拦截器已 toast */
  } finally {
    running.value = false
  }
}

const norm = (s: string) =>
  s
    .replace(/\r\n/g, '\n')
    .split('\n')
    .map((l) => l.trimEnd())
    .join('\n')
    .trim()

// 与期望输出的比对：整体归一（行尾空白/文末换行无关）；无期望输出时不判定
const pass = computed(() => {
  const r = result.value
  if (!r || !expected.value.trim()) return null
  return norm(r.stdout) === norm(expected.value) && r.status === 'Accepted'
})
</script>

<template>
  <div class="stest">
    <div class="stest-io">
      <div class="stest-block">
        <div class="stest-label">测试输入（stdin）</div>
        <el-input
          v-model="input"
          type="textarea"
          :rows="4"
          placeholder="程序运行时读取的输入"
          resize="none"
        />
      </div>
      <div class="stest-block">
        <div class="stest-label">期望输出（可选）</div>
        <el-input
          v-model="expected"
          type="textarea"
          :rows="4"
          placeholder="留空只展示运行结果"
          resize="none"
        />
      </div>
    </div>
    <div class="stest-act">
      <el-button type="primary" size="small" :loading="running" @click="run">运行自测</el-button>
      <span v-if="result" class="stest-meta">
        {{ result.status }} · {{ result.time_ms }}ms · {{ result.mem_kb }}KB
        <template v-if="result.truncated">（输出过长已截断）</template>
        <template v-if="pass !== null">
          ·
          <b :class="pass ? 'pass' : 'fail'">{{ pass ? '与期望输出一致 ✓' : '与期望输出不一致 ✗' }}</b>
        </template>
      </span>
    </div>
    <pre v-if="result?.stdout" class="stest-out">{{ result.stdout }}</pre>
    <pre v-if="result?.stderr" class="stest-err">{{ result.stderr }}</pre>
  </div>
</template>

<style scoped>
.stest {
  padding: 10px 12px;
  border-radius: 12px;
  background: #fff;
  border: 1px solid var(--soj-line, #e6eaf2);
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
  margin-top: 8px;
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
