<script setup lang="ts">
// 答案解析视频卡（v0.23）：链接制——B 站 BV 号转 iframe embed，mp4/webm 直链走 <video>，
// 其余（b23.tv 短链等）退化为外链按钮。可见性与题解同门槛（后端满分∨老师群体才下发 URL）。
import { computed } from 'vue'

const props = defineProps<{ url: string }>()

type VideoKind = 'bilibili' | 'file' | 'link'

const kind = computed<VideoKind>(() => {
  const u = props.url || ''
  if (/bilibili\.com\/video\/(BV[\w]+)/i.test(u)) return 'bilibili'
  if (/^https?:\/\/\S+\.(mp4|webm|ogg)(\?\S*)?$/i.test(u)) return 'file'
  return 'link'
})

const bvid = computed(() => {
  const m = props.url.match(/bilibili\.com\/video\/(BV[\w]+)/i)
  return m ? m[1] : ''
})

const embedSrc = computed(
  () => `//player.bilibili.com/player.html?bvid=${bvid.value}&autoplay=0&danmaku=0&high_quality=1`,
)
</script>

<template>
  <div class="ans-video">
    <iframe
      v-if="kind === 'bilibili'"
      :src="embedSrc"
      scrolling="no"
      frameborder="no"
      allowfullscreen="true"
      class="ans-frame"
    />
    <video v-else-if="kind === 'file'" :src="url" controls preload="metadata" class="ans-frame" />
    <a v-else :href="url" target="_blank" rel="noopener noreferrer" class="ans-link">
      ▶ 打开视频讲解（站外）
    </a>
  </div>
</template>

<style scoped>
.ans-frame {
  display: block;
  width: 100%;
  aspect-ratio: 16 / 9;
  border: 0;
  border-radius: 10px;
  background: #000;
}
.ans-link {
  display: block;
  padding: 12px 14px;
  border-radius: 10px;
  background: color-mix(in srgb, var(--soj-blue) 7%, #fff);
  color: var(--soj-blue);
  font-weight: var(--soj-fw-emphasis);
  text-decoration: none;
}
.ans-link:hover {
  background: color-mix(in srgb, var(--soj-blue) 13%, #fff);
}
</style>
