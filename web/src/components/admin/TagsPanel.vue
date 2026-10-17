<script setup lang="ts">
// 标签树管理（v0.5f，仅 root）：大纲式缩进文本编辑 + 实时预览 + 整树覆盖保存。
// 一行一个标签，缩进（两个空格或 Tab）表示下一级，空行忽略；
// 身份按名字对齐：同名标签保留 ID（题目引用不断），改名 = 删除旧名 + 新增新名，
// 旧名被题目引用时保存被拒并在下方列出引用题目明细。
import { computed, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { tagApi } from '@/api'
import type { TagNode, TagRefProblem } from '@/types'
import { useTagTreeStore } from '@/stores/tagtree'

const tagStore = useTagTreeStore()
const outline = ref('')
const saving = ref(false)
const blockedProblems = ref<TagRefProblem[] | null>(null)

interface ParsedNode {
  name: string
  children: ParsedNode[]
}

// 大纲解析：Tab 按 2 空格折算，行首空白宽度决定层级（相对上一行，回跳即回父级）
function parseOutline(text: string): { nodes: ParsedNode[]; error: string } {
  const root: ParsedNode = { name: '', children: [] }
  const stack: { node: ParsedNode; indent: number }[] = [{ node: root, indent: -1 }]
  const lines = text.split('\n')
  for (let i = 0; i < lines.length; i++) {
    const raw = lines[i]
    if (!raw.trim()) continue
    const expanded = raw.replace(/\t/g, '  ')
    const indent = expanded.length - expanded.trimStart().length
    const name = expanded.trim()
    if (name.length > 32) return { nodes: [], error: `第 ${i + 1} 行：标签超过 32 字` }
    while (stack.length > 1 && stack[stack.length - 1].indent >= indent) stack.pop()
    const node: ParsedNode = { name, children: [] }
    stack[stack.length - 1].node.children.push(node)
    stack.push({ node, indent })
  }
  return { nodes: root.children, error: '' }
}

const parsed = computed(() => parseOutline(outline.value))

const nodeCount = computed(() => {
  let n = 0
  const walk = (list: ParsedNode[]) => {
    for (const x of list) {
      n++
      walk(x.children)
    }
  }
  walk(parsed.value.nodes)
  return n
})

// 预览拍平成行（缩进表层级），免递归组件
const previewRows = computed(() => {
  const rows: { name: string; depth: number }[] = []
  const walk = (list: ParsedNode[], depth: number) => {
    for (const x of list) {
      rows.push({ name: x.name, depth })
      walk(x.children, depth + 1)
    }
  }
  walk(parsed.value.nodes, 0)
  return rows
})

function toApi(list: ParsedNode[]): TagNode[] {
  return list.map((n) => ({
    id: 0, // 身份由服务端按名字对齐分配
    name: n.name,
    ...(n.children.length ? { children: toApi(n.children) } : {}),
  }))
}

function toOutline(list: TagNode[], depth = 0): string {
  const pad = '  '.repeat(depth)
  return list
    .map((n) => pad + n.name + (n.children?.length ? '\n' + toOutline(n.children, depth + 1) : ''))
    .join('\n')
}

onMounted(async () => {
  await tagStore.ensure()
  outline.value = toOutline(tagStore.nodes)
})

async function onSave() {
  if (parsed.value.error) {
    ElMessage.warning(parsed.value.error)
    return
  }
  if (nodeCount.value > 500) {
    ElMessage.warning(`标签总数超过 500（当前 ${nodeCount.value}）`)
    return
  }
  try {
    await ElMessageBox.confirm(
      `将整树覆盖为 ${nodeCount.value} 个标签：同名标签保留引用不变，被删除的标签若仍被题目引用会拦截。确定保存？`,
      '保存标签树',
      { type: 'warning', confirmButtonText: '保存', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  saving.value = true
  blockedProblems.value = null
  try {
    const r = await tagApi.save(toApi(parsed.value.nodes))
    ElMessage.success(`标签树已保存（${r.count} 个标签）`)
    await tagStore.load()
  } catch (e) {
    // 拦截器已 toast 文案；这里取 failData 附带的引用题目明细做表格展示
    blockedProblems.value =
      (e as Error & { data?: { problems?: TagRefProblem[] } }).data?.problems ?? []
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <el-card>
    <template #header>
      <div class="panel-header">
        <span>标签树</span>
        <el-button type="primary" :loading="saving" @click="onSave">保存标签树</el-button>
      </div>
    </template>
    <el-alert type="info" :closable="false" class="howto">
      <p>一行一个标签，行首缩进（两个空格或 Tab）表示下一级，空行忽略；保存为整树覆盖。</p>
      <p>身份按名字对齐：同名标签保留 ID、题目引用不断；改名等于删除旧名＋新增新名，旧名被题目引用时保存会被拦截。</p>
    </el-alert>
    <div class="editor-row">
      <el-input
        v-model="outline"
        type="textarea"
        :rows="18"
        class="outline-input"
        placeholder="示例：&#10;算法&#10;  动态规划&#10;    树形 DP&#10;  图论&#10;    最短路&#10;数学&#10;  数论"
      />
      <div class="preview-col">
        <div class="dim preview-title">预览（{{ nodeCount }} 个标签）</div>
        <div v-if="parsed.error" class="parse-error">{{ parsed.error }}</div>
        <div v-else class="preview-tree">
          <div
            v-for="(row, i) in previewRows"
            :key="i"
            class="preview-row"
            :style="{ paddingLeft: row.depth * 20 + 8 + 'px' }"
          >
            {{ row.name }}
          </div>
          <div v-if="!previewRows.length" class="dim preview-empty">（空树：保存将清空标签树）</div>
        </div>
      </div>
    </div>
    <template v-if="blockedProblems">
      <el-alert
        type="error"
        :closable="false"
        class="blocked-alert"
        title="保存被拒：以下题目仍在使用将被删除的标签，请先在题目上解绑（编辑题目 → 标签）"
      />
      <el-table :data="blockedProblems" size="small">
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="title" label="题目" min-width="240" />
        <el-table-column label="使用的标签" min-width="260">
          <template #default="{ row }">
            <el-tag v-for="t in row.tags" :key="t" size="small" class="tag-gap">{{ t }}</el-tag>
            <span v-if="!row.tags?.length" class="dim">（无）</span>
          </template>
        </el-table-column>
      </el-table>
    </template>
  </el-card>
</template>

<style scoped>
.panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.howto {
  margin-bottom: 12px;
}
.howto p {
  margin: 2px 0;
}
.editor-row {
  display: flex;
  gap: 16px;
  align-items: stretch;
}
.outline-input {
  flex: 1;
}
.preview-col {
  flex: 1;
  min-width: 240px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 4px;
  background: var(--el-fill-color-lighter);
}
.preview-title {
  padding: 8px 8px 4px;
}
.parse-error {
  padding: 4px 8px;
  color: var(--el-color-danger);
}
.preview-tree {
  padding-bottom: 8px;
}
.preview-row {
  line-height: 26px;
}
.preview-empty {
  padding: 4px 8px;
}
.blocked-alert {
  margin-top: 12px;
}
.tag-gap {
  margin: 0 6px 4px 0;
}
</style>
