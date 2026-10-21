<script setup lang="ts">
import { fmtDate, fmtMem, fmtSec } from '@/utils/format'
import SubStatus from '@/components/SubStatus.vue'
import type { Submission } from '@/types'

withDefaults(
  defineProps<{
    modelValue: boolean
    title: string
    loading?: boolean
    list: Submission[]
    emptyText?: string
  }>(),
  { loading: false, emptyText: '该用户在此题没有提交' },
)

const emit = defineEmits<{ (e: 'update:modelValue', v: boolean): void }>()
</script>

<template>
  <el-dialog
    :model-value="modelValue"
    :title="title"
    width="1020"
    top="4vh"
    @update:model-value="(v: boolean) => emit('update:modelValue', v)"
  >
    <el-table stripe v-loading="loading" :data="list" border>
      <el-table-column type="expand">
        <template #default="{ row }">
          <div class="user-subs-detail">
            <pre v-if="row.code" class="mono sub-code">{{ row.code }}</pre>
            <el-table
              v-if="row.detail?.length || row.error_info"
              stripe
              :data="[...(row.detail ?? []), ...(row.error_info ? [{ msg: row.error_info }] : [])]"
              size="small"
              border
            >
              <el-table-column prop="index" label="测试点" width="90" />
              <el-table-column label="结果">
                <template #default="{ row: c }">
                  <SubStatus v-if="c.status" :status="c.status" />
                </template>
              </el-table-column>
              <el-table-column label="明细" min-width="150">
                <template #default="{ row: c }">
                  <span v-if="c.msg" class="case-err">{{ c.msg }}</span>
                </template>
              </el-table-column>
              <el-table-column label="耗时" width="120">
                <template #default="{ row: c }">{{ c.time_ms ? fmtSec(c.time_ms) : '' }}</template>
              </el-table-column>
              <el-table-column label="内存" width="120">
                <template #default="{ row: c }">{{ c.memory_kb ? fmtMem(c.memory_kb) : '' }}</template>
              </el-table-column>
            </el-table>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="状态" width="140">
        <template #default="{ row }">
          <SubStatus :status="row.status" :score="row.score" />
        </template>
      </el-table-column>
      <el-table-column prop="language" label="语言" width="100" />
      <el-table-column label="总耗时" width="100">
        <template #default="{ row }">{{ fmtSec(row.time_ms) }}</template>
      </el-table-column>
      <el-table-column label="总内存" width="110">
        <template #default="{ row }">{{ fmtMem(row.memory_kb) }}</template>
      </el-table-column>
      <el-table-column label="提交时间">
        <template #default="{ row }">{{ fmtDate(row.created_at) }}</template>
      </el-table-column>
    </el-table>
    <el-empty v-if="!loading && !list.length" :description="emptyText" :image-size="48" />
  </el-dialog>
</template>

<style scoped>
.user-subs-detail {
  padding: 8px 20px 16px;
}

.user-subs-detail .sub-code {
  margin-bottom: 12px;
}
</style>
