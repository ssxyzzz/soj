import { createPinia } from 'pinia'
import { createApp } from 'vue'
import ElementPlus from 'element-plus'
import zhCn from 'element-plus/dist/locale/zh-cn.mjs'
import 'element-plus/dist/index.css'
// vditor 样式静态引入（必须排在 main.css 之前）：vditor-reset 的排版规则（标题梯度/
// 代码块字号等）在 main.css 里统一维护，若随编辑器懒加载，样式插到 main.css 之后，
// 同优先级规则会反压 main.css——题面标题曾因此比页头标题还大（2026-10-18 修复）
import 'vditor/dist/index.css'

import App from './App.vue'
import router from './router'
import './styles/main.css'

const app = createApp(App)
app.use(createPinia())
app.use(router)
app.use(ElementPlus, { locale: zhCn })
app.mount('#app')
