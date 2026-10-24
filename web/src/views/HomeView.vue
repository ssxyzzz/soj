<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import MdView from '@/components/MdView.vue'

import { classApi, noticeApi } from '@/api'
import { useUserStore } from '@/stores/user'
import type { ClassCard } from '@/types'

const router = useRouter()
const userStore = useUserStore()
const classes = ref<ClassCard[]>([])
const notice = ref('')

const features = [
  {
    icon: '🖥️',
    tone: 'blue',
    title: '异步判题',
    desc: '提交后判题线程池从数据库原子领取任务，调用 go-judge 沙箱编译运行，Web 服务零阻塞。',
  },
  {
    icon: '🛡️',
    tone: 'teal',
    title: '安全沙箱',
    desc: 'go-judge 基于 namespace / cgroup 做时间、内存、进程数限制，隔离不可信的用户代码。',
  },
  {
    icon: '🏫',
    tone: 'violet',
    title: '班级教学',
    desc: '老师建班、发邀请码拉学生进班，班级页集中管理题目与成员，适合教学与训练。',
  },
  {
    icon: '🏆',
    tone: 'amber',
    title: '实时排行',
    desc: '解题榜按题去重即时更新，后续将支持 OI 赛制比赛排名。',
  },
]

const techs = ['Vue 3', 'TypeScript', 'Vite', 'Element Plus', 'Gin', 'MongoDB', 'go-judge', 'Docker']

onMounted(async () => {
  // 游客可见面收紧（v0.7.3）：班级接口需登录，游客跳过加载（班级卡片区自然隐藏）
  if (userStore.token) {
    try {
      const list = await classApi.list()
      classes.value = list.slice(0, 8)
    } catch {
      // 班级加载失败不影响首页其他内容
    }
  }
  try {
    notice.value = (await noticeApi.get()).content
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

    <!-- v0.19 hero（web-design-v3.md §4.3）：开放式居中区块（不再包卡片）——
         kicker 小标 + 大标题渐变点睛 + 胶囊按钮组，文案取自 v3-full 预览稿 -->
    <section class="hero">
      <div class="kicker">SANDBOX JUDGED · INSTANT SCORED</div>
      <h1 class="title">把每一次提交，<br />都变成一次<em>成长</em>。</h1>
      <p class="subtitle">面向班级与社团的轻量 Online Judge：提交代码，沙箱判题，实时出分。</p>
      <div class="actions">
        <el-button type="primary" size="large" @click="router.push('/problems')">开始刷题</el-button>
        <el-button size="large" plain @click="router.push('/classes')">查看班级</el-button>
      </div>
      <div class="techs">
        <el-tag v-for="t in techs" :key="t" size="small" class="tag">{{ t }}</el-tag>
      </div>
    </section>

    <el-row :gutter="16" class="features">
      <el-col v-for="f in features" :key="f.title" :xs="24" :sm="12" :md="6">
        <el-card class="feature">
          <div class="feature-icon" :class="`tone-${f.tone}`">{{ f.icon }}</div>
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

/* v0.19 hero：开放式居中区块（无卡片包裹，画布网格即背景） */
.hero {
  text-align: center;
  padding: 44px 0 36px;
  max-width: 720px;
  margin: 0 auto;
}

/* kicker 小标：主蓝大写字距标签（simple408 语言） */
.kicker {
  font-size: var(--soj-fs-small);
  font-weight: var(--soj-fw-bold);
  letter-spacing: 0.18em;
  color: var(--soj-blue);
  text-transform: uppercase;
  margin-bottom: 10px;
}

/* hero 标题：hero 档放大一档（38px）+ 藏蓝 800 + 负字距；「成长」青蓝渐变点睛 */
.title {
  margin: 0 0 14px;
  font-size: calc(var(--soj-fs-hero) * 1.27);
  font-weight: 800;
  letter-spacing: -1.2px;
  line-height: 1.24;
  color: var(--soj-navy);
}

.title em {
  font-style: normal;
  background: linear-gradient(100deg, var(--soj-blue-hi), var(--soj-teal));
  -webkit-background-clip: text;
  background-clip: text;
  color: transparent;
}

.subtitle {
  color: var(--el-text-color-regular);
  margin: 0 0 26px;
}

.actions {
  display: flex;
  justify-content: center;
  gap: 12px;
  margin-bottom: 20px;
}

.techs {
  display: flex;
  justify-content: center;
  flex-wrap: wrap;
  gap: 8px;
}

.features {
  margin-top: 8px;
}

.feature {
  text-align: center;
  height: 100%;
}

/* v0.19 特性图标：彩色圆角方块（emoji 居中其上），色调按 tone-× 四色 */
.feature-icon {
  width: 52px;
  height: 52px;
  border-radius: 14px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: calc(var(--soj-fs-emoji) * 0.72);
  line-height: 1;
}

.tone-blue {
  background: color-mix(in srgb, var(--soj-blue-hi) 12%, #ffffff);
}

.tone-teal {
  background: color-mix(in srgb, var(--soj-teal) 14%, #ffffff);
}

.tone-violet {
  background: color-mix(in srgb, #7c3aed 10%, #ffffff);
}

.tone-amber {
  background: color-mix(in srgb, #d98207 12%, #ffffff);
}

.feature h3 {
  margin: 10px 0 8px;
  font-size: var(--soj-fs-feature-title);
  font-weight: var(--soj-fw-bold);
  color: var(--soj-navy);
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

/* 实体名（班级名）：藏蓝（v0.19，原主蓝） */
.class-name {
  margin: 0 0 6px;
  color: var(--soj-navy);
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
