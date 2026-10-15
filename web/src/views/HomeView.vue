<script setup lang="ts">
import { useRouter } from 'vue-router'

const router = useRouter()

const features = [
  {
    icon: '🖥️',
    title: '异步判题',
    desc: '提交进入 RabbitMQ 队列，由独立 worker 消费并调用 go-judge 沙箱编译运行，Web 服务零阻塞。',
  },
  {
    icon: '🛡️',
    title: '安全沙箱',
    desc: 'go-judge 基于 namespace / cgroup 做时间、内存、进程数限制，隔离不可信的用户代码。',
  },
  {
    icon: '🏆',
    title: '实时排行',
    desc: 'Redis zset 维护解题排行榜，提交通过即刻更新，支持 O(logN) 的高效排名查询。',
  },
]

const techs = ['Vue 3', 'TypeScript', 'Vite', 'Element Plus', 'Gin', 'MySQL 8', 'Redis', 'RabbitMQ', 'go-judge', 'Docker']
</script>

<template>
  <div class="page">
    <el-card class="hero">
      <h1 class="title">SOJ 在线判题系统</h1>
      <p class="subtitle">从零搭建的 Online Judge：提交代码，沙箱判题，实时出分。</p>
      <div class="techs">
        <el-tag v-for="t in techs" :key="t" size="small" class="tag">{{ t }}</el-tag>
      </div>
      <div class="actions">
        <el-button type="primary" size="large" @click="router.push('/problems')">开始刷题</el-button>
        <el-button size="large" @click="router.push('/rank')">查看排行榜</el-button>
      </div>
    </el-card>

    <el-row :gutter="16" class="features">
      <el-col v-for="f in features" :key="f.title" :xs="24" :sm="8">
        <el-card class="feature">
          <div class="feature-icon">{{ f.icon }}</div>
          <h3>{{ f.title }}</h3>
          <p>{{ f.desc }}</p>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<style scoped>
.hero {
  text-align: center;
  padding: 32px 0;
}

.title {
  margin: 0 0 8px;
  font-size: 30px;
}

.subtitle {
  color: #606266;
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
  font-size: 36px;
}

.feature h3 {
  margin: 8px 0;
}

.feature p {
  color: #606266;
  font-size: 14px;
  line-height: 1.6;
}
</style>
