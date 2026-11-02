<script setup lang="ts">
// 自定义页面管理（v0.56）：后台直配零代码小页——外链（link）/内嵌（iframe，drawio 画板
// 协议壳）/Markdown 说明页。导航/页脚入口显隐 + 可见性（登录 | 指定角色）；slug 建后不可改，
// 排序相邻交换（新建默认排到末尾）。保存后 pages store refresh()，导航/页脚即时生效。
import { computed, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowDown, ArrowUp, Plus, Trash2 } from 'lucide-vue-next'

import MdEditor from '@/components/MdEditor.vue'
import { pagesApi } from '@/api'
import type { PageItem } from '@/types'
import { usePagesStore } from '@/stores/pages'

const loading = ref(true)
const pages = ref<PageItem[]>([])
const roleOptions = ref<{ id: string; name: string }[]>([])

async function load() {
  loading.value = true
  try {
    const r = await pagesApi.adminList()
    pages.value = r.pages || []
    roleOptions.value = r.roles || []
  } finally {
    loading.value = false
  }
}
onMounted(load)

const typeLabel: Record<string, string> = { link: '外链', iframe: '内嵌', markdown: '文档' }
const roleNames = computed(() => Object.fromEntries(roleOptions.value.map((r) => [r.id, r.name])))

function visLabel(p: PageItem) {
  if (p.visibility === 'login') return '登录可见'
  return '角色：' + p.roles.map((r) => roleNames.value[r] || r).join('、')
}

// ---- 新建/编辑弹窗 ----
const dialog = ref(false)
const saving = ref(false)
const editing = ref('') // 空 = 新建
const form = ref({
  slug: '',
  title: '',
  type: 'link' as PageItem['type'],
  url: '',
  proto: '' as '' | 'drawio',
  content: '',
  visibility: 'login' as 'login' | 'roles',
  roles: [] as string[],
  nav_show: true,
  footer_show: false,
  enabled: true,
  sort: 0,
})

function blankForm() {
  const lastSort = pages.value.length ? pages.value[pages.value.length - 1].sort : 0
  form.value = {
    slug: '', title: '', type: 'link', url: '', proto: '', content: '',
    visibility: 'login', roles: [],
    nav_show: true, footer_show: false, enabled: true,
    sort: lastSort + 10,
  }
}

function openCreate() {
  blankForm()
  editing.value = ''
  dialog.value = true
}

function openEdit(p: PageItem) {
  form.value = {
    slug: p.slug, title: p.title, type: p.type, url: p.url,
    proto: (p.proto || '') as '' | 'drawio',
    content: p.content || '',
    visibility: p.visibility, roles: [...(p.roles || [])],
    nav_show: p.nav_show, footer_show: p.footer_show,
    enabled: p.enabled, sort: p.sort,
  }
  editing.value = p.slug
  dialog.value = true
}

async function save() {
  const f = form.value
  const url = f.url.trim()
  if ((f.type === 'link' || f.type === 'iframe') && !/^https?:\/\//.test(url)) {
    ElMessage.warning('地址仅支持 http/https')
    return
  }
  if (f.visibility === 'roles' && !f.roles.length) {
    ElMessage.warning('指定角色可见时需至少勾选一个角色')
    return
  }
  saving.value = true
  try {
    const body = {
      title: f.title.trim(), type: f.type, url, proto: f.type === 'iframe' ? f.proto : '',
      content: f.type === 'markdown' ? f.content : '',
      visibility: f.visibility, roles: f.visibility === 'roles' ? f.roles : [],
      nav_show: f.nav_show, footer_show: f.footer_show, enabled: f.enabled, sort: f.sort,
    }
    if (editing.value) {
      await pagesApi.adminUpdate(editing.value, body)
    } else {
      await pagesApi.adminCreate({ ...body, slug: f.slug.trim() })
    }
    ElMessage.success('已保存')
    dialog.value = false
    await load()
    usePagesStore().refresh() // 导航/页脚入口即时生效
  } finally {
    saving.value = false
  }
}

async function removePage(p: PageItem) {
  try {
    await ElMessageBox.confirm(`删除「${p.title}」（${p.slug}）？入口与页面一并失效。`, '删除页面', {
      type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消',
    })
  } catch {
    return
  }
  await pagesApi.adminDelete(p.slug)
  ElMessage.success('已删除')
  await load()
  usePagesStore().refresh()
}

// 相邻交换 sort 值（默认档位 10/20/… 恒异值；两侧全量保存）
async function move(p: PageItem, dir: -1 | 1) {
  const i = pages.value.indexOf(p)
  const j = i + dir
  if (i < 0 || j < 0 || j >= pages.value.length) return
  const a = pages.value[i]
  const b = pages.value[j]
  const bodyOf = (x: PageItem, sort: number): Partial<PageItem> => ({
    title: x.title, type: x.type, url: x.url, proto: x.proto || '',
    content: x.content || '', visibility: x.visibility, roles: x.roles || [],
    nav_show: x.nav_show, footer_show: x.footer_show, enabled: x.enabled, sort,
  })
  await Promise.all([pagesApi.adminUpdate(a.slug, bodyOf(a, b.sort)), pagesApi.adminUpdate(b.slug, bodyOf(b, a.sort))])
  await load()
}
</script>

<template>
  <el-card>
    <template #header>
      <div class="head">
        <span>自定义页面</span>
        <el-button type="primary" :icon="Plus" @click="openCreate">新建页面</el-button>
      </div>
    </template>
    <el-table v-loading="loading" :data="pages" size="small">
      <el-table-column label="标题" min-width="140">
        <template #default="{ row }">
          {{ row.title }}
          <el-tag v-if="!row.enabled" type="info" size="small">停用</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="slug" label="Slug" min-width="100" />
      <el-table-column label="类型" width="110">
        <template #default="{ row }">
          {{ typeLabel[row.type] }}<template v-if="row.type === 'iframe' && row.proto === 'drawio'">·画板</template>
        </template>
      </el-table-column>
      <el-table-column label="可见性" min-width="140">
        <template #default="{ $index }">{{ visLabel(pages[$index]) }}</template>
      </el-table-column>
      <el-table-column label="导航" width="60" align="center">
        <template #default="{ row }">{{ row.nav_show ? '✓' : '—' }}</template>
      </el-table-column>
      <el-table-column label="页脚" width="60" align="center">
        <template #default="{ row }">{{ row.footer_show ? '✓' : '—' }}</template>
      </el-table-column>
      <el-table-column prop="sort" label="排序" width="70" align="center" />
      <el-table-column label="操作" width="230" align="center">
        <template #default="{ $index }">
          <el-button-group>
            <el-button size="small" @click="openEdit(pages[$index])">编辑</el-button>
            <el-button size="small" :icon="ArrowUp" :disabled="$index === 0" @click="move(pages[$index], -1)" />
            <el-button size="small" :icon="ArrowDown" :disabled="$index === pages.length - 1" @click="move(pages[$index], 1)" />
            <el-button size="small" type="danger" plain :icon="Trash2" @click="removePage(pages[$index])" />
          </el-button-group>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="dialog" :title="editing ? `编辑页面 · ${editing}` : '新建页面'" width="680px" destroy-on-close>
      <el-form label-position="top">
        <div class="grid2">
          <el-form-item label="标题（导航/页脚入口文字）">
            <el-input v-model="form.title" maxlength="24" placeholder="如：画板" />
          </el-form-item>
          <el-form-item label="Slug（URL 段，建后不可改）">
            <el-input v-model="form.slug" :disabled="!!editing" maxlength="32" placeholder="小写字母/数字/连字符，如 draw" />
          </el-form-item>
        </div>
        <el-form-item label="类型">
          <el-radio-group v-model="form.type">
            <el-radio-button value="link">外链</el-radio-button>
            <el-radio-button value="iframe">内嵌</el-radio-button>
            <el-radio-button value="markdown">Markdown 文档</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item v-if="form.type === 'link' || form.type === 'iframe'" label="地址（http/https）">
          <el-input v-model="form.url" maxlength="512" :placeholder="form.type === 'iframe' ? 'https://example.com/tool' : 'https://example.com'" />
          <span v-if="form.type === 'iframe' && form.proto === 'drawio'" class="dim">
            drawio 画板填 base 地址（如 https://embed.diagrams.net），壳自动拼接 embed 参数
          </span>
        </el-form-item>
        <el-form-item v-if="form.type === 'iframe'" label="内嵌协议">
          <el-radio-group v-model="form.proto">
            <el-radio value="">无（普通网页）</el-radio>
            <el-radio value="drawio">drawio 画板（导出/暂存/丢弃）</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item v-if="form.type === 'markdown'" label="内容（Markdown）">
          <MdEditor v-model="form.content" height="300px" />
        </el-form-item>
        <el-form-item label="可见性">
          <div class="vis-row">
            <el-radio-group v-model="form.visibility">
              <el-radio value="login">登录可见</el-radio>
              <el-radio value="roles">指定角色</el-radio>
            </el-radio-group>
            <el-checkbox-group v-if="form.visibility === 'roles'" v-model="form.roles" class="roles">
              <el-checkbox v-for="r in roleOptions" :key="r.id" :value="r.id">{{ r.name }}</el-checkbox>
            </el-checkbox-group>
          </div>
        </el-form-item>
        <div class="grid3">
          <el-form-item label="导航入口">
            <el-switch v-model="form.nav_show" />
          </el-form-item>
          <el-form-item label="页脚入口">
            <el-switch v-model="form.footer_show" />
          </el-form-item>
          <el-form-item label="启用">
            <el-switch v-model="form.enabled" />
          </el-form-item>
          <el-form-item label="排序值（小者靠前）">
            <el-input-number v-model="form.sort" :min="0" :max="1000000" :controls="false" style="width: 110px" />
          </el-form-item>
        </div>
      </el-form>
      <template #footer>
        <el-button @click="dialog = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="save">保存</el-button>
      </template>
    </el-dialog>
  </el-card>
</template>

<style scoped>
.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.grid2 {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: var(--soj-sp-16);
}

.grid3 {
  display: grid;
  grid-template-columns: repeat(4, auto);
  gap: var(--soj-sp-16);
  justify-content: start;
}

.vis-row {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-16);
  flex-wrap: wrap;
}

.roles {
  display: flex;
  gap: var(--soj-sp-8);
  flex-wrap: wrap;
}

.dim {
  display: block;
  width: 100%;
  color: var(--soj-ink-3);
  font-size: var(--soj-fs-12);
}
</style>
