<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'

import ProblemForm from '@/components/ProblemForm.vue'
import type { ProblemFormPayload } from '@/components/ProblemForm.vue'
import { problemApi } from '@/api'

// 网页建题：表单字段/布局/校验在 ProblemForm 共享组件（编辑页复用同一套），
// 本页只保留提交流程：元数据 + 题面 + 测试数据 zip 一次 FormData 交齐。
const router = useRouter()
const saving = ref(false)

async function onSubmit(p: ProblemFormPayload) {
  saving.value = true
  try {
    const fd = new FormData()
    fd.append('title', p.title)
    // 来源四字段（v0.12）：所有题型共用，服务端校验后直传导入管道
    fd.append('source_type', p.source_type || '')
    fd.append('source', p.source || '')
    fd.append('year', String(p.year || 0))
    fd.append('region', p.region || '')
    // 函数题（v0.9.1）：type + driver/template 文件；zip 可选（含 cases）
    if (p.type === 'function') {
      fd.append('type', 'function')
      fd.append('statement', p.statement)
      fd.append('tag_ids', JSON.stringify(p.tag_ids))
      fd.append('difficulty', String(p.difficulty))
      if (p.driver) fd.append('driver', p.driver)
      if (p.template) fd.append('template', p.template)
      if (p.zip) fd.append('zip', p.zip)
      const r = await problemApi.create(fd)
      ElMessage.success(`题目 #${r.id} 创建成功`)
      router.push(`/problems/${r.id}`)
      return
    }
    // 交互题（v0.9.2）：type + interactor 文件；zip 可选（含 cases，ans 可缺）
    if (p.type === 'interactive') {
      fd.append('type', 'interactive')
      fd.append('statement', p.statement)
      fd.append('tag_ids', JSON.stringify(p.tag_ids))
    fd.append('difficulty', String(p.difficulty))
      if (p.interactor) fd.append('interactor', p.interactor)
      if (p.zip) fd.append('zip', p.zip)
      const r = await problemApi.create(fd)
      ElMessage.success(`题目 #${r.id} 创建成功`)
      router.push(`/problems/${r.id}`)
      return
    }
    // 客观题（v0.9）：type + question_json，无 zip/时限/比对
    if (p.type === 'objective') {
      fd.append('type', 'objective')
      fd.append('statement', p.statement)
      fd.append('tag_ids', JSON.stringify(p.tag_ids))
    fd.append('difficulty', String(p.difficulty))
      fd.append('questions_json', JSON.stringify(p.questions))
      const r = await problemApi.create(fd)
      ElMessage.success(`题目 #${r.id} 创建成功`)
      router.push(`/problems/${r.id}`)
      return
    }
    fd.append('time_limit_ms', String(p.time_limit_ms))
    fd.append('memory_limit_kb', String(p.memory_limit_mb * 1024))
    fd.append('compare', p.compare)
    if (p.compare === 'tolerant') {
      fd.append('float_tolerance', String(p.float_tolerance))
    }
    fd.append('statement', p.statement)
    if (p.file_io_name) fd.append('file_io_name', p.file_io_name)
    fd.append('tag_ids', JSON.stringify(p.tag_ids))
    fd.append('difficulty', String(p.difficulty))
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
  <ProblemForm
    mode="create"
    :saving="saving"
    confirm-text="确认创建"
    @submit="onSubmit"
    @cancel="router.push('/problems')"
  >
    <template #aside-bottom>
      <el-card shadow="never">
        <template #header><span>建题须知</span></template>
        <div class="tips">
          <p>创建后题目默认公开可见，可在题目详情页对学生隐藏。</p>
          <p>题面在上方编辑器里写——数据 zip 只认 data/ 目录（数据对保持原名 + config.yaml + gen.py 等附属文件），包内不收题面；带题面的题包请走题库页「导入题目」。</p>
          <p>测试点按文件名末段数字从小到大计分（单点等分）；子任务分组与校验器可由 data/config.yaml 声明；样例直接写在题面里。</p>
          <p>题面可直接粘贴截图，图片自动上传并插入引用。</p>
          <p>客观题（单选/多选/判断/填空）在「题目类型」切换：题干写在题面里、选项与答案在下方配置；填空题用连续 4 个下划线（____）标记空位。</p>
          <p>比对模式创建后可在题目详情「编辑题目」中调整。</p>
        </div>
      </el-card>
    </template>
  </ProblemForm>
</template>
