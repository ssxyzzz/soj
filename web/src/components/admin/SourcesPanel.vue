<script setup lang="ts">
// 来源库面板（v0.12，root）：来源名登记册——表单 datalist 建议、导入未匹配 tag 的
// 归队缓冲。引用数按题目侧实时聚合（source 字段 ∪ rawTags）；改名跟随题目；
// 「转为标签」把引用题目批量改挂标签树节点（指定已有节点或新建同名节点）。
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { sourcesApi } from '@/api'
import { useTagTreeStore } from '@/stores/tagtree'
import { SOURCE_TYPE_OPTIONS, sourceTypeLabel } from '@/utils/source'
import type { SourceEntry } from '@/types'

const tagStore = useTagTreeStore()

const list = ref<SourceEntry[]>([])
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    list.value = (await sourcesApi.adminList()).list
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  load()
  tagStore.ensure()
})

// ---- 新增 / 改名改型 弹窗 ----

const showEdit = ref(false)
const editing = ref<SourceEntry | null>(null) // null = 新增
const editForm = reactive({ name: '', type: '' })
const savingEdit = ref(false)

function openCreate() {
  editing.value = null
  editForm.name = ''
  editForm.type = ''
  showEdit.value = true
}

function openEdit(row: SourceEntry) {
  editing.value = row
  editForm.name = row.name
  editForm.type = row.type
  showEdit.value = true
}

async function saveEdit() {
  if (!editForm.name.trim()) {
    ElMessage.warning('请填写来源名')
    return
  }
  savingEdit.value = true
  try {
    if (editing.value) {
      await sourcesApi.update(editing.value.id, {
        name: editForm.name.trim(),
        type: editForm.type,
      })
      ElMessage.success('来源已更新（改名会同步跟随引用题目）')
    } else {
      await sourcesApi.create(editForm.name.trim(), editForm.type)
      ElMessage.success('来源已登记')
    }
    showEdit.value = false
    await load()
  } finally {
    savingEdit.value = false
  }
}

// 类型快捷改型（表格内下拉）：仅改型，不动名称
async function changeType(row: SourceEntry, type: string) {
  await sourcesApi.update(row.id, { type })
  ElMessage.success(`「${row.name}」已归类为${sourceTypeLabel(type) || '未分类'}`)
  await load()
}

async function removeRow(row: SourceEntry) {
  try {
    await ElMessageBox.confirm(
      `删除登记「${row.name}」？题目上的来源字段与原标签不受影响（仅不再出现在建议与面板）。`,
      '删除来源',
      { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  await sourcesApi.remove(row.id)
  ElMessage.success('已删除')
  await load()
}

// ---- 转为标签 ----

const showToTag = ref(false)
const toTagRow = ref<SourceEntry | null>(null)
// mode: new = 新建同名节点（可选父节点，0 = 根级）；existing = 挂到已有节点
const toTagForm = reactive({ mode: 'new' as 'new' | 'existing', parent_id: 0, node_id: undefined as number | undefined, name: '' })
const converting = ref(false)

function openToTag(row: SourceEntry) {
  toTagRow.value = row
  toTagForm.mode = 'new'
  toTagForm.parent_id = 0
  toTagForm.node_id = undefined
  toTagForm.name = row.name
  showToTag.value = true
}

async function convertToTag() {
  const row = toTagRow.value
  if (!row) return
  if (toTagForm.mode === 'existing' && !toTagForm.node_id) {
    ElMessage.warning('请选择要挂载的标签节点')
    return
  }
  try {
    await ElMessageBox.confirm(
      `将把引用「${row.name}」的 ${row.refs} 道题目改挂标签节点，并从来源库移除该条目。确认？`,
      '转为标签',
      { type: 'warning', confirmButtonText: '转换', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  converting.value = true
  try {
    const r = await sourcesApi.toTag(row.id, {
      ...(toTagForm.mode === 'existing'
        ? { node_id: toTagForm.node_id }
        : { parent_id: toTagForm.parent_id, name: toTagForm.name.trim() || row.name }),
    })
    ElMessage.success(`已转标签：${r.updated} 道题目改挂节点 #${r.node_id}`)
    showToTag.value = false
    tagStore.invalidate() // 树可能新增了节点，全局刷新
    await load()
  } finally {
    converting.value = false
  }
}

// el-tree-select 数据：根级补一个「根级（一级标签）」虚拟节点（id=0）供选父节点
const parentTree = () => [
  { id: 0, name: '根级（一级标签）', children: tagStore.nodes },
]
</script>

<template>
  <el-card v-loading="loading">
    <template #header>
      <div class="panel-head">
        <span>来源库</span>
        <div class="head-actions">
          <el-button size="small" @click="load">刷新</el-button>
          <el-button size="small" type="primary" @click="openCreate">新增来源</el-button>
        </div>
      </div>
    </template>

    <div class="dim panel-tip">
      来源名在建题表单输入与题包导入（未匹配标签的 tag）时自动登记；「转为标签」把知识点类来源归队到标签树。
    </div>

    <el-table :data="list" stripe>
      <el-table-column prop="name" label="名称" min-width="200" />
      <el-table-column label="类型" width="160">
        <template #default="{ row }">
          <el-select
            :model-value="row.type"
            size="small"
            @change="(t: string) => changeType(row as SourceEntry, t)"
          >
            <el-option value="" label="未分类" />
            <el-option v-for="t in SOURCE_TYPE_OPTIONS" :key="t.value" :value="t.value" :label="t.label" />
          </el-select>
        </template>
      </el-table-column>
      <el-table-column label="引用题数" width="100">
        <template #default="{ row }">
          <span :class="{ dim: !row.refs }">{{ row.refs }}</span>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="230">
        <template #default="{ row }">
          <el-button size="small" text type="primary" @click="openEdit(row as SourceEntry)">改名</el-button>
          <el-button size="small" text type="primary" @click="openToTag(row as SourceEntry)">转为标签</el-button>
          <el-button size="small" text type="danger" @click="removeRow(row as SourceEntry)">删除</el-button>
        </template>
      </el-table-column>
      <template #empty>暂无来源登记（建题表单输入来源名后会自动沉淀到这里）</template>
    </el-table>

    <!-- 新增 / 改名改型 -->
    <el-dialog v-model="showEdit" :title="editing ? '编辑来源' : '新增来源'" width="480px">
      <el-form label-position="top">
        <el-form-item label="来源名" required>
          <el-input v-model="editForm.name" maxlength="64" show-word-limit placeholder="如 NOIP、USACO 1.3.2" />
        </el-form-item>
        <el-form-item label="类型">
          <el-select v-model="editForm.type" clearable placeholder="未分类" style="width: 100%">
            <el-option v-for="t in SOURCE_TYPE_OPTIONS" :key="t.value" :value="t.value" :label="t.label" />
          </el-select>
        </el-form-item>
      </el-form>
      <div v-if="editing" class="dim dialog-tip">改名会同步替换引用题目上的来源名与原标签。</div>
      <template #footer>
        <el-button @click="showEdit = false">取消</el-button>
        <el-button type="primary" :loading="savingEdit" @click="saveEdit">保存</el-button>
      </template>
    </el-dialog>

    <!-- 转为标签 -->
    <el-dialog v-model="showToTag" :title="`转为标签：${toTagRow?.name ?? ''}`" width="520px">
      <el-form label-position="top">
        <el-form-item label="目标节点">
          <el-radio-group v-model="toTagForm.mode">
            <el-radio value="new">新建同名标签</el-radio>
            <el-radio value="existing">指定已有节点</el-radio>
          </el-radio-group>
        </el-form-item>
        <template v-if="toTagForm.mode === 'new'">
          <el-form-item label="新节点名称">
            <el-input v-model="toTagForm.name" maxlength="32" placeholder="默认取来源名，可修正" />
          </el-form-item>
          <el-form-item label="挂载位置（父节点）">
            <el-tree-select
              v-model="toTagForm.parent_id"
              :data="parentTree()"
              :render-after-expand="false"
              check-strictly
              default-expand-all
              style="width: 100%"
            />
          </el-form-item>
        </template>
        <el-form-item v-else label="选择标签节点">
          <el-tree-select
            v-model="toTagForm.node_id"
            :data="tagStore.nodes"
            :render-after-expand="false"
            check-strictly
            default-expand-all
            style="width: 100%"
          />
        </el-form-item>
      </el-form>
      <div class="dim dialog-tip">
        引用该来源的题目（{{ toTagRow?.refs ?? 0 }} 道）将补挂所选节点；题目上保留的原标签名随之移除，来源字段不受影响。
      </div>
      <template #footer>
        <el-button @click="showToTag = false">取消</el-button>
        <el-button type="primary" :loading="converting" @click="convertToTag">转换</el-button>
      </template>
    </el-dialog>
  </el-card>
</template>

<style scoped>
.panel-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.head-actions {
  display: flex;
  gap: 8px;
}

.panel-tip {
  margin-bottom: 12px;
}

.dialog-tip {
  line-height: 1.6;
}
</style>
