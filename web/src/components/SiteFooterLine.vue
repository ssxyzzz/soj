<script setup lang="ts">
// 页脚信息行（2026-11-02 用户拍板单行化 + 主题改「点击向上选择」）：主布局与登录页共用。
// 顺序 = SOJ·版本（外链仓库）| 主题（popover 向上弹出 v5/原版）| 技术栈行 | ICP | 公安网备
// | 自定义页面入口（v0.56，后台「自定义页面」配置）。备案号空 = 不展示。
import { computed, ref } from 'vue'

import { applyTheme, initTheme, type ThemeName } from '@/utils/theme'
import { APP_VERSION } from '@/utils/version'
import { useSiteStore } from '@/stores/site'
import { usePagesStore } from '@/stores/pages'

const site = useSiteStore()
const pagesStore = usePagesStore()

// 主题（2026-10-26）：v5 版（默认）/ 原版，data-theme 整站变量换血；popover 向上弹（用户拍板）
const theme = ref<ThemeName>(initTheme())
const pop = ref()
function switchTheme(t: ThemeName) {
  applyTheme(t)
  theme.value = t
  pop.value?.hide()
}

const themes: { value: ThemeName; label: string }[] = [
  { value: 'v5', label: 'v5 版' },
  { value: 'navy', label: '原版' },
]

// 公安网备查询链接：备案号中的连续数字即查询 code（如 皖公网安备 34010000000001号）；
// 提不出数字时退回查询页首页
const mpsLink = computed(() => {
  const code = (site.mpsNumber || '').replace(/\D/g, '')
  return code
    ? `https://beian.mps.gov.cn/#/query/webSearch?code=${code}`
    : 'https://beian.mps.gov.cn/'
})
</script>

<template>
  <div class="sfl">
    <!-- 页脚版本与仓库链接：SOJ+版本整体外链仓库；版本号发版时改 utils/version.ts 一处 -->
    <a class="flink" href="https://github.com/ssxyzzz/soj" target="_blank" rel="noopener noreferrer">SOJ · {{ APP_VERSION }}</a>
    <span class="fsep">|</span>
    <!-- 主题：点击「主题」向上弹出两档选择（拍板：点击后向上选择） -->
    <el-popover ref="pop" placement="top" :width="132" trigger="click" popper-class="sfl-theme-pop">
      <template #reference>
        <button type="button" class="theme-btn">
          主题
          <svg class="caret" width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round"><path d="M6 15l6-6 6 6" /></svg>
        </button>
      </template>
      <div class="theme-list">
        <button
          v-for="t in themes"
          :key="t.value"
          type="button"
          :class="{ on: theme === t.value }"
          @click="switchTheme(t.value)"
        >
          {{ t.label }}
        </button>
      </div>
    </el-popover>
    <span class="fsep">|</span>
    <span>Gin + Vue 3 + MongoDB + go-judge 在线判题系统</span>
    <span class="fsep">|</span>
    <!-- ICP 备案号（v0.13）：后台「系统设置」配置，空 = 不展示；法规要求链接工信部备案系统 -->
    <a
      v-if="site.icpNumber"
      class="icp"
      href="https://beian.miit.gov.cn/"
      target="_blank"
      rel="noopener noreferrer"
    >{{ site.icpNumber }}</a>
    <!-- 公安网备号（2026-11-02）：同后台配置；备案号中数字即查询 code，链接公安部备案查询；
         前缀 ghs 徽标（/ghs.png 公安备案图标，惯例名）随号展示 -->
    <a
      v-if="site.mpsNumber"
      class="icp"
      :href="mpsLink"
      target="_blank"
      rel="noopener noreferrer"
    ><img class="ghs" src="/ghs.png" alt="公安备案" />{{ site.mpsNumber }}</a>
    <!-- 自定义页面入口（v0.56）：后台「自定义页面」配置；link 型站外新开，其余走 /p/:slug -->
    <template v-for="p in pagesStore.footerPages" :key="p.slug">
      <span class="fsep">|</span>
      <a
        v-if="p.type === 'link'"
        :href="p.url"
        target="_blank"
        rel="noopener noreferrer"
        class="icp"
      >{{ p.title }}</a>
      <router-link v-else :to="`/p/${p.slug}`" class="icp">{{ p.title }}</router-link>
    </template>
  </div>
</template>

<style scoped>
.sfl {
  display: flex;
  align-items: center;
  justify-content: center;
  flex-wrap: wrap;
  gap: var(--soj-sp-16);
  color: var(--soj-ink-3);
}

.fsep {
  color: var(--soj-line);
}

.flink {
  color: var(--soj-ink-3);
  text-decoration: none;
}

.flink:hover {
  color: var(--soj-blue);
}

/* 主题触发钮：文字 + 上箭头，弱化灰、悬停提主蓝 */
.theme-btn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  border: none;
  background: transparent;
  /* 字体三件套显式继承（font: inherit 的 font-size: inherit 会撞 v0.57 stylelint
     字号白名单——字号一律走 --soj-fs-* token）；随页脚 12px 档 */
  font-family: inherit;
  font-size: var(--soj-fs-12);
  font-weight: inherit;
  color: var(--soj-ink-3);
  cursor: pointer;
  padding: 0;
}

.theme-btn:hover,
.theme-btn:hover .caret {
  color: var(--soj-blue);
}

.caret {
  color: var(--el-text-color-placeholder);
}

/* 弹层内的两档选择（v5 版 / 原版），当前项高亮 */
.theme-list {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.theme-list button {
  border: none;
  background: transparent;
  font: inherit;
  font-size: var(--soj-fs-13);
  color: var(--soj-ink-2);
  text-align: left;
  padding: 7px 10px;
  border-radius: var(--el-border-radius-base);
  cursor: pointer;
}

.theme-list button:hover {
  background: var(--el-fill-color-light);
  color: var(--soj-blue);
}

.theme-list button.on {
  color: var(--soj-blue);
  background: var(--soj-blue-soft);
  font-weight: var(--soj-fw-emphasis);
}

/* 备案号外链：弱化为页脚次要信息，悬停才提色 */
.icp {
  color: var(--soj-ink-3);
  text-decoration: none;
}

.icp:hover {
  color: var(--soj-blue);
}

/* 公安备案徽标：随公安网备号前缀展示，行内对齐 */
.ghs {
  width: 14px;
  height: 16px;
  object-fit: contain;
  vertical-align: -3px;
  margin-right: 3px;
}
</style>
