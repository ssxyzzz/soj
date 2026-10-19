<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import MdView from '@/components/MdView.vue'

import { classApi, noticeApi } from '@/api'
import type { ClassCard } from '@/types'

const router = useRouter()
const classes = ref<ClassCard[]>([])
const notice = ref('')

const features = [
  {
    icon: '🖥️',
    title: '异步判题',
    desc: '提交后判题线程池从数据库原子领取任务，调用 go-judge 沙箱编译运行，Web 服务零阻塞。',
  },
  {
    icon: '🛡️',
    title: '安全沙箱',
    desc: 'go-judge 基于 namespace / cgroup 做时间、内存、进程数限制，隔离不可信的用户代码。',
  },
  {
    icon: '🏫',
    title: '班级教学',
    desc: '老师建班、发邀请码拉学生进班，班级页集中管理题目与成员，适合教学与训练。',
  },
  {
    icon: '🏆',
    title: '实时排行',
    desc: '解题榜按题去重即时更新，后续将支持 OI 赛制比赛排名。',
  },
]

const techs = ['Vue 3', 'TypeScript', 'Vite', 'Element Plus', 'Gin', 'MongoDB', 'go-judge', 'Docker']

onMounted(async () => {
  try {
    const list = await classApi.list()
    classes.value = list.slice(0, 8)
  } catch {
    // 班级加载失败不影响首页其他内容
  }
  try {
    notice.value = (await noticeApi.get()).content_html
  } catch {
    // 公告加载失败不影响首页其他内容
  }
})
</script>

<template>
  <div class="page">
    <el-card v-if="notice" class="notice">
      <template #header><span>📢 公告</span></template>
      <MdView :content="notice" />
    </el-card>

    <el-card class="hero">
      <h1 class="title">SOJ 在线判题系统</h1>
      <p class="subtitle">面向班级与社团的轻量 Online Judge：提交代码，沙箱判题，实时出分。</p>
      <div class="techs">
        <el-tag v-for="t in techs" :key="t" size="small" class="tag">{{ t }}</el-tag>
      </div>
      <div class="actions">
        <el-button type="primary" size="large" plain @click="router.push('/problems')">开始刷题</el-button>
        <el-button size="large" @click="router.push('/classes')">查看班级</el-button>
      </div>
    </el-card>

    <el-row :gutter="16" class="features">
      <el-col v-for="f in features" :key="f.title" :xs="24" :sm="12" :md="6">
        <el-card class="feature">
          <div class="feature-icon">{{ f.icon }}</div>
          <h3>{{ f.title }}</h3>
          <p>{{ f.desc }}</p>
        </el-card>
      </el-col>
    </el-row>

    <el-card v-if="classes.length" class="classes">
      <template #header>
        <div class="classes-head">
          <span>班级</span>
          <el-link type="primary" @click="router.push('/classes')">全部班级</el-link>
        </div>
      </template>
      <el-row :gutter="16">
        <el-col v-for="c in classes" :key="c.id" :xs="24" :sm="12" :md="6">
          <el-card class="class-card" shadow="hover" @click="router.push(`/classes/${c.id}`)">
            <h4 class="class-name">{{ c.name }}</h4>
            <p class="class-intro">{{ c.intro || '暂无简介' }}</p>
            <div class="class-meta">
              <el-tag size="small" type="info">{{ c.member_count }} 名成员</el-tag>
              <el-tag size="small" type="info">{{ c.assignment_count }} 个作业</el-tag>
            </div>
          </el-card>
        </el-col>
      </el-row>
    </el-card>
  </div>
</template>

<style scoped>
.notice {
  margin-bottom: 16px;
}

.hero {
  text-align: center;
  padding: 32px 0;
}

/* hero 标题 30/600（强调档最高级） */
.title {
  margin: 0 0 8px;
  font-size: var(--soj-fs-hero);
  font-weight: var(--soj-fw-emphasis);
}

.subtitle {
  color: var(--el-text-color-regular);
  margin: 0 0 16px;
}

.techs {
  display: flex;
  justify-content: center;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 24px;
}

.actions {
  display: flex;
  justify-content: center;
  gap: 12px;
}

.features {
  margin-top: 16px;
}

.feature {
  text-align: center;
  height: 100%;
}

.feature-icon {
  font-size: var(--soj-fs-emoji);
}

.feature h3 {
  margin: 8px 0;
  font-size: var(--soj-fs-feature-title);
  font-weight: var(--soj-fw-emphasis);
}

.feature p {
  color: var(--el-text-color-regular);
  line-height: 1.6;
}

.classes {
  margin-top: 16px;
}

.classes-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.class-card {
  cursor: pointer;
  margin-bottom: 12px;
}

/* 实体名（班级名）主色 */
.class-name {
  margin: 0 0 6px;
  color: var(--el-color-primary);
  font-size: var(--soj-fs-card-title);
  font-weight: var(--soj-fw-bold);
}

.class-intro {
  color: var(--el-text-color-secondary);
  height: 36px;
  overflow: hidden;
  text-overflow: ellipsis;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}

.class-meta {
  display: flex;
  gap: 8px;
  margin-top: 8px;
}
</style>
