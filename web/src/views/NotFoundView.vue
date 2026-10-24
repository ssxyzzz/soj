<script setup lang="ts">
import { useRouter } from 'vue-router'

const router = useRouter()
</script>

<template>
  <!-- v3-full .nf：居中渐变大字 404 + 说明 + 回首页主钮 -->
  <div class="page">
    <div class="nf">
      <div class="code">404</div>
      <div class="msg">页面不存在或已被移除</div>
      <el-button type="primary" size="large" @click="router.push('/')">回到首页</el-button>
    </div>
  </div>
</template>

<style scoped>
.nf {
  min-height: 68vh;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  text-align: center;
}

/* v3：120° 蓝青渐变裁字大数字 */
.code {
  font-size: 100px;
  font-weight: var(--soj-fw-heavy);
  letter-spacing: -5px;
  line-height: 1;
  background: linear-gradient(120deg, var(--soj-blue-hi), var(--soj-teal));
  -webkit-background-clip: text;
  background-clip: text;
  color: transparent;
}

.msg {
  font-size: var(--soj-fs-16);
  color: var(--soj-ink-2);
}
</style>
