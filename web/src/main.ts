import { createPinia } from 'pinia'
import { createApp } from 'vue'

// Element Plus 按需（v0.28 打包批）：模板组件由 unplugin-vue-components 自动按需
// 引入（含样式），全量注册与 dist/index.css 退役；此处只补模板解析器覆盖不到的
// 样式——程序化调用的 ElMessage / ElMessageBox 与 v-loading 指令（直接 import JS
// 不带样式）。中文 locale 移至 App.vue 的 ElConfigProvider。
import 'element-plus/es/components/message-box/style/css'
import 'element-plus/es/components/message/style/css'
import 'element-plus/es/components/loading/style/css'

import App from './App.vue'
import router from './router'
import './styles/main.css'
import { initTheme } from './utils/theme'

initTheme() // 主题（默认 v5 新案）在挂载前落到 <html data-theme>，避免首帧闪色

const app = createApp(App)
app.use(createPinia())
app.use(router)
app.mount('#app')
