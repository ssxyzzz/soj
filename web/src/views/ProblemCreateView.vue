<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'

import PageShell from '@/components/PageShell.vue'
import ProblemForm from '@/components/ProblemForm.vue'
import type { ProblemFormPayload } from '@/components/ProblemForm.vue'
import { problemApi } from '@/api'

// 网页建题（v0.5b-1）：表单字段/布局/校验在 ProblemForm 共享组件（编辑题目复用同一套），
// 本页只保留提交流程：元数据 + 题面 + 测试数据 zip 一次 FormData 交齐。
const router = useRouter()
const saving = ref(false)

async function onSubmit(p: ProblemFormPayload) {
  saving.value = true
  try {
    const fd = new FormData()
    fd.append('title', p.title)
    fd.append('time_limit_ms', String(p.time_limit_ms))
    fd.append('memory_limit_kb', String(p.memory_limit_mb * 1024))
    fd.append('compare', p.compare)
    if (p.compare === 'tolerant') {
      fd.append('float_tolerance', String(p.float_tolerance))
    }
    fd.append('statement', p.statement)
    if (p.file_io_name) fd.append('file_io_name', p.file_io_name)
    fd.append('zip', p.zip!)
    if (p.checker) fd.append('checker', p.checker)
    const r = await problemApi.create(fd)
    ElMessage.success(`题目 #${r.id} 创建成功`)
    router.push(`/problems/${r.id}`)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <PageShell>
    <el-card v-loading="saving">
      <template #header><span>新建题目</span></template>
      <ProblemForm mode="create" :saving="saving" @submit="onSubmit" />
    </el-card>

    <template #aside>
      <el-card shadow="never">
        <template #header><span>说明</span></template>
        <div class="tips">
          <p>创建后题目默认公开可见，可在题目详情页对学生隐藏。</p>
          <p>测试点按 cases/ 下目录编号从小到大计分（单点等分）；子任务分组与样例标记暂不支持网页配置。</p>
          <p>题面可直接粘贴截图，图片自动上传并插入引用。</p>
          <p>比对模式创建后可在题目详情「编辑题目」中调整。</p>
        </div>
      </el-card>
    </template>
  </PageShell>
</template>

<style scoped>
.tips {
  font-size: 15px;
  color: #606266;
  line-height: 1.8;
}

.tips p {
  margin: 0 0 8px;
}
</style>
