<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { QuestionFilled } from '@element-plus/icons-vue'
import type { UploadUserFile } from 'element-plus'
import { ElMessage } from 'element-plus'

import MdEditor from '@/components/MdEditor.vue'
import PageShell from '@/components/PageShell.vue'
import { problemApi } from '@/api'

// 网页建题（v0.5b-1）：元数据表单 + 题面 MdEditor + 测试数据 zip。
// zip 只需 cases/<N>/in|ans 结构（N 为测试点编号），problem.yaml 由表单字段生成。
const router = useRouter()
const saving = ref(false)
const form = reactive({
  title: '',
  time_limit_ms: 1000,
  memory_limit_mb: 256,
  compare: 'exact',
  float_tolerance: 0.0001,
  statement: '',
  file_io_name: '',
})
const fileList = ref<UploadUserFile[]>([])
const checkerList = ref<UploadUserFile[]>([])

function onCheckerChange(_file: UploadUserFile, files: UploadUserFile[]) {
  checkerList.value = files.slice(-1)
}

function onCheckerRemove(_file: UploadUserFile, files: UploadUserFile[]) {
  checkerList.value = files
}

function checkerFile(): File | null {
  const f = checkerList.value[0]
  return (f?.raw as File) ?? null
}

function onZipChange(_file: UploadUserFile, files: UploadUserFile[]) {
  // 只保留最新选择的一个 zip
  fileList.value = files.slice(-1)
}

function onZipRemove(_file: UploadUserFile, files: UploadUserFile[]) {
  fileList.value = files
}

function zipFile(): File | null {
  const f = fileList.value[0]
  return (f?.raw as File) ?? null
}

async function submit() {
  if (!form.title.trim()) {
    ElMessage.warning('请填写标题')
    return
  }
  if (!form.statement.trim()) {
    ElMessage.warning('请填写题面')
    return
  }
  const zip = zipFile()
  if (!zip) {
    ElMessage.warning('请上传测试数据 zip（cases/<N>/in|ans 结构）')
    return
  }
  saving.value = true
  try {
    const fd = new FormData()
    fd.append('title', form.title.trim())
    fd.append('time_limit_ms', String(form.time_limit_ms))
    fd.append('memory_limit_kb', String(form.memory_limit_mb * 1024))
    fd.append('compare', form.compare)
    if (form.compare === 'tolerant') {
      fd.append('float_tolerance', String(form.float_tolerance))
    }
    fd.append('statement', form.statement)
    if (form.file_io_name.trim()) fd.append('file_io_name', form.file_io_name.trim())
    fd.append('zip', zip)
    const ck = checkerFile()
    if (ck) fd.append('checker', ck)
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
      <el-form label-position="top">
        <el-form-item label="标题" required>
          <el-input v-model="form.title" maxlength="64" show-word-limit placeholder="64 字符以内" />
        </el-form-item>
        <el-form-item label="时限 / 内存">
          <div class="limits">
            <el-input-number v-model="form.time_limit_ms" :min="100" :max="600000" :step="100" />
            <span class="dim">ms</span>
            <el-input-number v-model="form.memory_limit_mb" :min="16" :max="1024" :step="16" />
            <span class="dim">MB</span>
          </div>
        </el-form-item>
        <el-form-item label="题面" required>
          <MdEditor v-model="form.statement" height="420px" />
        </el-form-item>
        <el-form-item label="测试数据 zip" required>
          <el-upload
            v-model:file-list="fileList"
            class="zip-upload"
            drag
            accept=".zip"
            :limit="1"
            :auto-upload="false"
            :on-change="onZipChange"
            :on-remove="onZipRemove"
          >
            <div class="zip-hint">
              <p>拖入 zip 或点击选择</p>
              <p class="dim">结构：cases/1/in、cases/1/ans、cases/2/in……每个测试点一对输入/答案文件</p>
            </div>
          </el-upload>
        </el-form-item>
        <el-form-item>
          <el-tooltip
            placement="top"
            content="全文比对：输出与答案精确一致（忽略行尾空白与文末换行）；宽容比对：按空白分词比较，数字可带精度容差（适合答案不唯一的构造/计算题）"
          >
            <span class="compare-label">
              比对模式
              <el-icon><QuestionFilled /></el-icon>
            </span>
          </el-tooltip>
          <div>
            <el-radio-group v-model="form.compare">
              <el-radio-button value="exact">全文比对</el-radio-button>
              <el-radio-button value="tolerant">宽容比对</el-radio-button>
            </el-radio-group>
            <span class="dim ml8">
              {{ form.compare === 'tolerant' ? '按空白分词比较，数字可带精度容差（适合答案不唯一的构造/计算题）' : '输出与答案精确一致（忽略行尾空白与文末换行）' }}
            </span>
          </div>
        </el-form-item>
        <el-form-item v-if="form.compare === 'tolerant'" label="浮点容差">
          <el-input-number v-model="form.float_tolerance" :min="0.000001" :max="1" :step="0.0001" :precision="6" />
        </el-form-item>
        <el-form-item label="自定义校验器（可选）">
          <div>
            <el-upload
              v-model:file-list="checkerList"
              accept=".cpp"
              :limit="1"
              :auto-upload="false"
              :on-change="onCheckerChange"
              :on-remove="onCheckerRemove"
            >
              <el-button>选择 checker.cpp</el-button>
            </el-upload>
            <div class="dim checker-tip">
              配置后判题由校验器决定（调用约定：checker 输入 期望 考生输出，退出码 0 = 通过）。也可创建后在题目页管理。
            </div>
          </div>
        </el-form-item>
        <el-form-item>
          <div class="fileio-block">
            <div class="fileio-title">
              文件 IO 文件名（可选）
              <span class="dim">仅比赛/考试「本场启用文件 IO」时生效；留空默认 problem，输入文件即 problem.in、输出即 problem.out</span>
            </div>
            <el-input
              v-model="form.file_io_name"
              placeholder="problem"
              style="width: 280px"
              maxlength="64"
            />
          </div>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" :loading="saving" @click="submit">创建题目</el-button>
        </el-form-item>
      </el-form>
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
.limits {
  display: flex;
  align-items: center;
  gap: 8px;
}

.fileio-block {
  width: 100%;
}

.fileio-title {
  font-weight: 600;
  margin-bottom: 8px;
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.fileio-block :deep(.el-form-item__content) {
  display: block;
}

.fileio-block .limits {
  margin-bottom: 8px;
}

/* zip 拖拽区撑满表单，与题面编辑器同宽（zip-upload 类落在 el-upload 根元素上） */
.zip-upload {
  width: 100%;
}

.zip-upload :deep(.el-upload-dragger) {
  width: 100%;
}

.compare-label {
  cursor: help;
  color: #606266;
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

.zip-hint {
  padding: 8px;
}

.zip-hint p {
  margin: 2px 0;
}

.tips {
  font-size: 13px;
  color: #606266;
  line-height: 1.8;
}

.tips p {
  margin: 0 0 8px;
}

.ml8 {
  margin-left: 8px;
}
</style>
