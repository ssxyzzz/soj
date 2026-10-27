<script setup lang="ts">
// 题目榜单弹窗（v0.23 Tab 化，收编原「所有代码」）：挑战榜 / 用时榜 / 内存榜 / 全部代码。
//   - 挑战榜登录即可见（榜单只含用时与名字，不泄题）；用时/内存榜维持满分门槛，
//     未解锁时 Tab 内提示（can_view_boards 由服务端判定）；
//   - 全部代码 = 原有列表 + 详情交互（排序三档/语言/提交者筛选，点行进详情）；
//   - 榜单渲染做不可见字符过滤（TopsCoding 用户名 RTL 控制符伪装翻车教训）。
import { computed, ref, watch } from 'vue'

import { problemApi, userApi } from '@/api'
import CodePane from '@/components/CodePane.vue'
import SubStatus from '@/components/SubStatus.vue'
import { fmtDate, fmtMem, fmtRel, fmtSec, statusDesc } from '@/utils/format'
import { languageOptions } from '@/utils/code'
import type { BoardsResult, LookupUser, Submission } from '@/types'

const props = defineProps<{
  modelValue: boolean
  problemId: number
  problemTitle: string
}>()
const emit = defineEmits<{ (e: 'update:modelValue', v: boolean): void }>()

const PAGE_SIZE = 10
type BoardTab = 'challenge' | 'time' | 'mem' | 'codes'
const tab = ref<BoardTab>('challenge')
const boards = ref<BoardsResult | null>(null)
const boardsLoading = ref(false)

// ---------- 全部代码（原所有代码） ----------
const others = ref<Submission[]>([])
const othersTotal = ref(0)
const othersPage = ref(1)
const othersSort = ref<'recent' | 'time' | 'mem'>('recent')
const othersLang = ref('')
// 提交者筛选（2026-10-25 用户需求「选定账号查找」）：远程模糊搜账号，选中按 user_id 过滤
const othersUser = ref<number | undefined>()
const userOptions = ref<LookupUser[]>([])
const userSearching = ref(false)
const othersView = ref<'list' | 'detail'>('list')
const othersDetail = ref<Submission | null>(null)
// 测试点按题目数据顺序展示（防后端返回顺序波动）；系统级错误（沙箱失败等）并入表尾明细行
const sortedDetail = computed(() => {
  const rows = [...(othersDetail.value?.detail ?? [])].sort((a, b) => a.index - b.index)
  const info = othersDetail.value?.error_info
  if (info && !/^第 \d+ 个测试点/.test(info)) rows.push({ msg: info } as never)
  return rows
})

// 不可见字符过滤（v0.23）：RTL 覆盖符/零宽字符等控制符从展示名中剔除
const RTL_OVERRIDES = /[\u200e\u200f\u202a-\u202e\u2066-\u2069\u200b-\u200d\u2060\ufeff]/g
function safeName(row: { uid?: number; nickname?: string; username?: string }): string {
  const clean = (s?: string) => (s ?? '').replace(RTL_OVERRIDES, '')
  return clean(row.nickname) || clean(row.username) || `#${row.uid ?? ''}`
}

async function searchUsers(q: string) {
  if (!q.trim()) return // 保留候选，防已选中项显示成 uid 数字
  userSearching.value = true
  try {
    userOptions.value = await userApi.lookup(q.trim())
  } catch {
    userOptions.value = []
  } finally {
    userSearching.value = false
  }
}

// 最后一页不足 PAGE_SIZE 时补空白行，让表格保持整页高度
const displayOthers = computed(() => {
  const fill = Math.max(0, PAGE_SIZE - others.value.length)
  return [...others.value, ...Array.from({ length: fill }, () => ({}) as Submission)]
})

async function loadOthers(p = 1) {
  try {
    const r = await problemApi.submissions(
      props.problemId,
      p,
      PAGE_SIZE,
      othersSort.value,
      othersLang.value || undefined,
      othersUser.value || undefined,
    )
    others.value = r.list
    othersTotal.value = r.total
    othersPage.value = p
  } catch {
    others.value = []
  }
}

async function loadBoards() {
  boardsLoading.value = true
  try {
    boards.value = await problemApi.boards(props.problemId)
  } catch {
    boards.value = null
  } finally {
    boardsLoading.value = false
  }
}

watch(
  () => props.modelValue,
  (open) => {
    if (open) {
      tab.value = 'challenge' // 默认挑战榜：低门槛即参与钩子
      othersUser.value = undefined // 换题打开重置提交者筛选
      othersView.value = 'list'
      othersDetail.value = null
      loadBoards()
      loadOthers()
    }
  },
)

function openOthersDetail(s: Submission) {
  if (!s.id) return // 补位的空白行不可点
  othersDetail.value = s
  othersView.value = 'detail'
}
</script>

<template>
  <el-dialog
    :model-value="modelValue"
    title="题目榜单"
    width="1020"
    top="4vh"
    @update:model-value="(v: boolean) => emit('update:modelValue', v)"
    @closed="othersView = 'list'"
  >
    <el-tabs v-model="tab" class="board-tabs">
      <!-- 挑战榜 -->
      <el-tab-pane label="挑战榜" name="challenge">
        <el-table v-loading="boardsLoading" stripe :data="boards?.challenge ?? []" border>
          <el-table-column prop="rank" label="名次" width="70">
            <template #default="{ row }">
              <span class="rk" :class="`rk-${row.rank <= 3 ? row.rank : 'n'}`">{{ row.rank }}</span>
            </template>
          </el-table-column>
          <el-table-column label="挑战者" min-width="150">
            <template #default="{ row }">
              <router-link :to="`/users/${row.uid}`" class="board-user">{{ safeName(row) }}</router-link>
            </template>
          </el-table-column>
          <el-table-column label="用时" width="110">
            <template #default="{ row }">
              <span class="mono">{{ fmtSec(row.duration_ms) }}</span>
            </template>
          </el-table-column>
          <el-table-column label="码字速度" width="110">
            <template #default="{ row }">
              <span class="mono">{{ row.chars_per_sec ? row.chars_per_sec.toFixed(1) : '—' }}</span>
              <span class="dim"> 字/秒</span>
            </template>
          </el-table-column>
          <el-table-column prop="lang" label="语言" width="110" />
          <el-table-column label="达成时间">
            <template #default="{ row }">{{ fmtRel(row.created_at) }}</template>
          </el-table-column>
        </el-table>
        <el-empty
          v-if="boards && !boards.challenge?.length"
          description="还没有挑战记录——点「⚡ 挑战模式」抢首杀"
          :image-size="48"
        />
      </el-tab-pane>

      <!-- 用时榜 -->
      <el-tab-pane label="用时榜" name="time">
        <div v-if="boards && !boards.can_view_boards" class="board-locked">
          该题拿到满分后可查看用时榜与内存榜（挑战榜不受限）
        </div>
        <template v-else>
          <el-table v-loading="boardsLoading" stripe :data="boards?.time ?? []" border>
            <el-table-column prop="rank" label="名次" width="70">
              <template #default="{ row }">
                <span class="rk" :class="`rk-${row.rank <= 3 ? row.rank : 'n'}`">{{ row.rank }}</span>
              </template>
            </el-table-column>
            <el-table-column label="选手" min-width="150">
              <template #default="{ row }">
                <router-link :to="`/users/${row.uid}`" class="board-user">{{ safeName(row) }}</router-link>
              </template>
            </el-table-column>
            <el-table-column label="最短用时" width="110">
              <template #default="{ row }">
                <span class="mono">{{ fmtSec(row.value) }}</span>
              </template>
            </el-table-column>
            <el-table-column prop="lang" label="语言" width="110" />
            <el-table-column label="达成时间">
              <template #default="{ row }">{{ fmtRel(row.created_at) }}</template>
            </el-table-column>
          </el-table>
          <el-empty v-if="boards && !boards.time?.length" description="还没有可计入的通过提交" :image-size="48" />
        </template>
      </el-tab-pane>

      <!-- 内存榜 -->
      <el-tab-pane label="内存榜" name="mem">
        <div v-if="boards && !boards.can_view_boards" class="board-locked">
          该题拿到满分后可查看用时榜与内存榜（挑战榜不受限）
        </div>
        <template v-else>
          <el-table v-loading="boardsLoading" stripe :data="boards?.mem ?? []" border>
            <el-table-column prop="rank" label="名次" width="70">
              <template #default="{ row }">
                <span class="rk" :class="`rk-${row.rank <= 3 ? row.rank : 'n'}`">{{ row.rank }}</span>
              </template>
            </el-table-column>
            <el-table-column label="选手" min-width="150">
              <template #default="{ row }">
                <router-link :to="`/users/${row.uid}`" class="board-user">{{ safeName(row) }}</router-link>
              </template>
            </el-table-column>
            <el-table-column label="最小内存" width="110">
              <template #default="{ row }">
                <span class="mono">{{ fmtMem(row.value) }}</span>
              </template>
            </el-table-column>
            <el-table-column prop="lang" label="语言" width="110" />
            <el-table-column label="达成时间">
              <template #default="{ row }">{{ fmtRel(row.created_at) }}</template>
            </el-table-column>
          </el-table>
          <el-empty v-if="boards && !boards.mem?.length" description="还没有可计入的通过提交" :image-size="48" />
        </template>
      </el-tab-pane>

      <!-- 全部代码（原「所有代码」） -->
      <el-tab-pane label="全部代码" name="codes">
        <template v-if="othersView === 'list'">
          <div class="dlg-toolbar">
            <el-radio-group v-model="othersSort" @change="loadOthers(1)">
              <el-radio-button value="recent">最近提交</el-radio-button>
              <el-radio-button value="time">时间优先</el-radio-button>
              <el-radio-button value="mem">空间优先</el-radio-button>
            </el-radio-group>
            <el-select
              v-model="othersLang"
              clearable
              placeholder="全部语言"
              style="width: 160px"
              @change="loadOthers(1)"
            >
              <el-option v-for="o in languageOptions" :key="o.value" :value="o.value" :label="o.label" />
            </el-select>
            <!-- 提交者筛选（2026-10-25 用户需求「选定账号查找」）：远程模糊搜账号 -->
            <el-select
              v-model="othersUser"
              class="user-sel"
              filterable
              remote
              clearable
              reserve-keyword
              placeholder="全部提交者"
              :remote-method="searchUsers"
              :loading="userSearching"
              @change="loadOthers(1)"
            >
              <el-option v-for="u in userOptions" :key="u.id" :value="u.id" :label="u.nickname || u.username" />
            </el-select>
            <span class="dim">共 {{ othersTotal }} 份</span>
          </div>
          <el-table
            stripe
            :data="displayOthers"
            size="small"
            border
            style="cursor: pointer"
            @row-click="(row: Submission) => openOthersDetail(row as Submission)"
          >
            <el-table-column label="状态" width="140">
              <template #default="{ row }">
                <template v-if="row.id">
                  <el-tooltip v-if="statusDesc(row.status)" placement="top" effect="light">
                    <template #content>
                      <div class="status-tip">
                        <div>{{ statusDesc(row.status)!.meaning }}</div>
                        <div v-if="statusDesc(row.status)!.cause">常见原因：{{ statusDesc(row.status)!.cause }}</div>
                      </div>
                    </template>
                    <SubStatus :status="row.status" :score="row.score" />
                  </el-tooltip>
                  <SubStatus v-else :status="row.status" :score="row.score" />
                </template>
              </template>
            </el-table-column>
            <el-table-column prop="username" label="提交者" min-width="110" />
            <el-table-column prop="language" label="语言" width="100" />
            <el-table-column label="总耗时" width="100">
              <template #default="{ row }">
                <template v-if="row.id">{{ fmtSec(row.time_ms) }}</template>
              </template>
            </el-table-column>
            <el-table-column label="总内存" width="110">
              <template #default="{ row }">
                <template v-if="row.id">{{ fmtMem(row.memory_kb) }}</template>
              </template>
            </el-table-column>
            <el-table-column label="提交时间">
              <template #default="{ row }">
                <template v-if="row.id">{{ fmtRel(row.created_at) }}</template>
              </template>
            </el-table-column>
          </el-table>
          <el-empty v-if="!others.length" description="还没有人提交过" :image-size="48" />
          <div v-if="othersTotal > 0" class="side-pager">
            <el-pagination
              background
              layout="prev, pager, next"
              :total="othersTotal"
              :page-size="PAGE_SIZE"
              :current-page="othersPage"
              @current-change="(p: number) => loadOthers(p)"
            />
          </div>
        </template>
        <template v-else-if="othersDetail">
          <el-button class="mb8" @click="othersView = 'list'">← 返回列表</el-button>
          <el-descriptions :column="3" border class="detail-desc">
            <el-descriptions-item label="状态">
              <el-tooltip v-if="statusDesc(othersDetail.status)" placement="top" effect="light">
                <template #content>
                  <div class="status-tip">
                    <div>{{ statusDesc(othersDetail.status)!.meaning }}</div>
                    <div v-if="statusDesc(othersDetail.status)!.cause">常见原因：{{ statusDesc(othersDetail.status)!.cause }}</div>
                  </div>
                </template>
                <SubStatus :status="othersDetail.status" :score="othersDetail.score" />
              </el-tooltip>
              <SubStatus v-else :status="othersDetail.status" :score="othersDetail.score" />
            </el-descriptions-item>
            <el-descriptions-item label="提交者">{{ othersDetail.username }}</el-descriptions-item>
            <el-descriptions-item label="语言">{{ othersDetail.language }}</el-descriptions-item>
            <el-descriptions-item label="总耗时">{{ fmtSec(othersDetail.time_ms) }}</el-descriptions-item>
            <el-descriptions-item label="总内存">{{ fmtMem(othersDetail.memory_kb) }}</el-descriptions-item>
            <el-descriptions-item label="提交时间">{{ fmtDate(othersDetail.created_at) }}</el-descriptions-item>
          </el-descriptions>
          <!-- 有测试点明细时错误原因已在明细列展示（表尾 err 行），仅无明细时独立成块（如编译错误） -->
          <template v-if="othersDetail.error_info && !sortedDetail.length">
            <div class="detail-sub">错误信息</div>
            <pre class="mono sub-code">{{ othersDetail.error_info }}</pre>
          </template>
          <div class="detail-sub">代码</div>
          <CodePane :code="othersDetail.code" :language="othersDetail.language" />
          <template v-if="othersDetail.detail?.length">
            <div class="detail-sub">测试点明细</div>
            <el-table stripe :data="sortedDetail" border>
              <el-table-column prop="index" label="测试点" width="90" />
              <el-table-column label="结果">
                <template #default="{ row }">
                  <SubStatus v-if="row.status" :status="row.status" />
                </template>
              </el-table-column>
              <el-table-column label="明细" min-width="150">
                <template #default="{ row }">
                  <span v-if="row.msg" class="case-err">{{ row.msg }}</span>
                </template>
              </el-table-column>
              <el-table-column label="耗时" width="90">
                <template #default="{ row }">{{ fmtSec(row.time_ms) }}</template>
              </el-table-column>
              <el-table-column label="内存" width="90">
                <template #default="{ row }">{{ row.memory_kb ? fmtMem(row.memory_kb) : '' }}</template>
              </el-table-column>
            </el-table>
          </template>
          <template v-else-if="othersDetail.error_info">
            <div class="detail-sub">错误信息</div>
            <pre class="mono sub-code">{{ othersDetail.error_info }}</pre>
          </template>
        </template>
      </el-tab-pane>
    </el-tabs>
  </el-dialog>
</template>

<style scoped>
.dlg-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
}

/* 提交者筛选下拉 */
.user-sel {
  width: 170px;
}

.board-tabs :deep(.el-tabs__header) {
  margin-bottom: 14px;
}

.board-user {
  color: var(--soj-ink);
  text-decoration: none;
  font-weight: 600;
}
.board-user:hover {
  color: var(--soj-blue);
}

/* 三甲名次配色 */
.rk {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 24px;
  height: 24px;
  padding: 0 6px;
  border-radius: 8px;
  font-weight: 800;
  font-size: 13px;
  color: var(--soj-ink-2);
  background: var(--soj-bg, #f2f4f8);
}
.rk-1 {
  color: #92400e;
  background: color-mix(in srgb, #f59e0b 22%, #fff);
}
.rk-2 {
  color: #475569;
  background: color-mix(in srgb, #94a3b8 26%, #fff);
}
.rk-3 {
  color: #7c2d12;
  background: color-mix(in srgb, #b45309 16%, #fff);
}

.board-locked {
  padding: 42px 0;
  text-align: center;
  color: var(--soj-ink-3);
  font-size: 14px;
}

.dim {
  font-size: 12.5px;
  color: var(--soj-ink-3);
}
</style>
