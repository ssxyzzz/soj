import { createPinia } from 'pinia'
import { createApp } from 'vue'
import ElementPlus from 'element-plus'
import zhCn from 'element-plus/dist/locale/zh-cn.mjs'
import 'element-plus/dist/index.css'
// KaTeX 样式字体与 highlight.js 主题静态引入（自托管，vite 资产管线带 hash，零 CDN）：
// 查看端数学/高亮懒加载的是 JS，样式必须常驻避免首屏公式闪版
import 'katex/dist/katex.min.css'
import 'highlight.js/styles/github.css'

import App from './App.vue'
import router from './router'
import './styles/main.css'

const app = createApp(App)
app.use(createPinia())
app.use(router)
app.use(ElementPlus, { locale: zhCn })
app.mount('#app')
