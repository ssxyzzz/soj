// Element Plus 程序化 API 的统一出口（v0.28 打包批）。
//
// 为什么存在：`import { ElMessage } from 'element-plus'` 走根入口，实测会让 rollup
// 把全量组件（145+ 个，943KB）打进产物、摇树失效；子路径导入才能只保留用到的部分。
// vite.config 里已把裸导入 'element-plus' 精确别名到本文件（正则 ^element-plus$，
// 不影响 'element-plus/es/**'、'element-plus/dist/**' 与 '@element-plus/icons-vue'）。
//
// 维护口径：需要新的程序化组件（如 ElNotification）时，在这里补一行子路径再导出，
// 否则构建会以「本模块未导出该名字」显式报错，不会静默回退到全量包。
export { ElMessage } from 'element-plus/es/components/message/index.mjs'
export { ElMessageBox } from 'element-plus/es/components/message-box/index.mjs'
