<script setup lang="ts">
// 从题单复制（v0.18.3 拍板 7）：班级作业选题区旁的入口——选题单后按章节序拍平、
// 隐藏题由服务端一律剔除并计数（与观察者权限无关），合并进已选题目（按题号去重）。
// 合并逻辑在父级（emit merge），组件只管选单与取数。
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'

import { listApi } from '@/api'
import type { ListCard } from '@/types'

const emit = defineEmits<{ (e: 'merge', items: { id: number; title: string }[], skippedHidden: number): void }>()

const show = ref(false)
const lists = ref<ListCard[]>([])
const listId = ref<number | null>(null)
const loading = ref(false)

onMounted(async () => {
  try {
    const r = await listApi.all()
    lists.value = r.items.filter((l) => l.problem_count > 0)
  } catch {
    lists.value = []
  }
})

async function confirm() {
  if (!listId.value) {
    ElMessage.warning('先选一个题单')
    return
  }
  loading.value = true
  try {
    const r = await listApi.flat(listId.value)
    emit('merge', r.items, r.skipped_hidden)
    show.value = false
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <span class="copy-wrap">
    <el-button size="small" @click="show = true">从题单复制</el-button>
    <el-dialog v-model="show" title="从题单复制题目" width="460" append-to-body>
      <el-select v-model="listId" filterable placeholder="选择题单" style="width: 100%">
        <el-option v-for="l in lists" :key="l.id" :value="l.id" :label="`${l.title}（${l.problem_count} 题）`" />
      </el-select>
      <p class="hint">
        按题单章节顺序拍平合并进「已选」；同一题只保留一次；隐藏题自动跳过并提示数量。
      </p>
      <template #footer>
        <el-button @click="show = false">取消</el-button>
        <el-button type="primary" :loading="loading" @click="confirm">复制</el-button>
      </template>
    </el-dialog>
  </span>
</template>

<style scoped>
.hint {
  margin: 10px 0 0;
  font-size: 12.5px;
  color: var(--soj-ink-3, #5b6b85);
}
</style>
