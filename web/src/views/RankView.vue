<script setup lang="ts">
import { onMounted, ref } from 'vue'

import { rankApi } from '@/api'
import type { RankItem } from '@/types'

const list = ref<RankItem[]>([])
const loading = ref(false)

const medals = ['🥇', '🥈', '🥉']

onMounted(async () => {
  loading.value = true
  try {
    list.value = await rankApi.top()
  } finally {
    loading.value = false
  }
})
</script>

<template>
  <div class="page">
    <el-card>
      <template #header><span>解题排行榜（前 50 名）</span></template>
      <el-table v-loading="loading" :data="list">
        <el-table-column label="排名" width="100">
          <template #default="{ row }">
            <span v-if="row.rank <= 3">{{ medals[row.rank - 1] }}</span>
            <span v-else>{{ row.rank }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="username" label="用户名" min-width="160" />
        <el-table-column prop="nickname" label="昵称" min-width="160" />
        <el-table-column label="解题数" width="120">
          <template #default="{ row }">
            <el-tag type="success">{{ row.solved }}</el-tag>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && !list.length" description="还没有人通过题目，快去抢占第一！" />
    </el-card>
  </div>
</template>
