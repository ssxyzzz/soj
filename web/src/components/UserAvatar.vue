<script setup lang="ts">
// 用户头像（v0.100.2）：昵称首字圆，或 QQ 头像。
//
// 首字圆恒在底层，QQ 头像加载成功才盖上去——加载中、被墙失败（校园网墙掉 qlogo，v0.37
// 顶栏同款场景）都停在首字圆：不露白圆、不裂图。直链只有一个来源：服务端 qqAvatarURL
//（设置页「QQ头像」开关开且填了 QQ 号才下发），与顶栏、公开主页是同一条 URL，浏览器命中
// 同一份缓存。
//
// 外观（尺寸 / 底色 / 字号）由使用方的类定在根节点上；这里只管结构、圆形裁切与图片盖层。
// 换人 = 换直链：榜单点表头换排序时同一实例会被复用，图元按直链重建、加载状态按直链
// 判定——上一个人的图和加载进度都不带到下一个人身上。
import { computed, ref } from 'vue'

const props = withDefaults(
  defineProps<{
    src?: string | null // QQ 头像直链；空 = 只有首字圆
    name: string // 昵称 / 用户名，取首字
    lazy?: boolean // 图片懒加载（整页百行的表格开；首屏内的领奖台、顶栏不开）
  }>(),
  { src: '', lazy: false },
)

// 首字原样取用、不转大小写：全站统一为昵称本身的大小写（2026-11-25 用户拍板）
const char = computed(() => (props.name || '?').slice(0, 1))

// 加载状态按「哪条直链」记，不按当前实例记：换人后被替下的旧图元若还在途，它的 load /
// error 会迟到——只认当前图元自己的事件，算不到新图元头上。（真机复现过两种翻车：按实例
// 记时，旧图元迟到的 load 把还在途的新图元提前点亮；只按直链记时，它又能把已经显示的
// 新图元熄灭。）
const imgEl = ref<HTMLImageElement>() // 当前图元（按直链重建）
const loadedSrc = ref('') // 已加载完成的那条直链
const brokenSrc = ref('') // 加载失败的那条直链（停在首字圆，不重试——换直链才算新人）
const loaded = computed(() => !!props.src && loadedSrc.value === props.src)
const broken = computed(() => !!props.src && brokenSrc.value === props.src)

function onLoaded(e: Event) {
  const el = e.currentTarget as HTMLImageElement
  if (el !== imgEl.value) return // 被替下的旧图元：迟到的事件不作数
  loadedSrc.value = el.getAttribute('src') ?? ''
}
function onBroken(e: Event) {
  const el = e.currentTarget as HTMLImageElement
  if (el !== imgEl.value) return
  brokenSrc.value = el.getAttribute('src') ?? ''
}
</script>

<template>
  <!-- 装饰件：名字就在旁边（昵称 / 顶栏用户名），读屏不必再念一遍首字 -->
  <span class="uavatar" aria-hidden="true">
    <span class="uavatar-char">{{ char }}</span>
    <!-- key 绑直链：换人必须换新图元——沿用旧图元只改 src，上一个人的图会多留一拍
         （真机实测：去掉 key 后换排序 80ms 仍有 2～3 个位置可见旧图） -->
    <img
      v-if="src && !broken"
      :key="src"
      ref="imgEl"
      :src="src"
      alt=""
      class="uavatar-img"
      :class="{ 'is-loaded': loaded }"
      :loading="lazy ? 'lazy' : 'eager'"
      decoding="async"
      @load="onLoaded"
      @error="onBroken"
    />
  </span>
</template>

<style scoped>
/* 首字圆：同径圆 + 品牌渐变（同顶栏头像）；overflow hidden 把图片盖层裁成圆 */
.uavatar {
  position: relative; /* 图片盖层的定位基准 */
  display: inline-flex;
  flex: none;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  overflow: hidden;
  line-height: 1;
  color: var(--soj-white);
  font-weight: var(--soj-fw-bold);
  background: linear-gradient(135deg, var(--soj-blue-hi), var(--soj-teal));
  user-select: none;
}

/* 图片盖层：铺满圆、加载完成前透明（首字圆透出）；细描边同顶栏 / 主页的 QQ 头像版。
   描边是半透明的，图片必须自带白底——否则描边下面透出的是首字圆的品牌渐变，
   成了一整圈蓝青色环（真机放大实锤） */
.uavatar-img {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  box-sizing: border-box;
  object-fit: cover;
  border: 1px solid var(--soj-line);
  border-radius: 50%;
  background: var(--soj-white);
  opacity: 0;
  transition: opacity 0.2s;
}

.uavatar-img.is-loaded {
  opacity: 1;
}

@media (prefers-reduced-motion: reduce) {
  .uavatar-img {
    transition: none;
  }
}
</style>
