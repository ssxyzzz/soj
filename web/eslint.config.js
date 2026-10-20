// v0.5.4 ④：ESLint 引入（推荐规则集，不做激进风格配置——风格交给现有约定）
// vue-tsc 管类型，ESLint 管未用导入/变量、Vue 语义规则与可维护性。
import pluginVue from 'eslint-plugin-vue'
import vueTsEslintConfig from '@vue/eslint-config-typescript'
import globals from 'globals'

export default [
  { ignores: ['dist/**', 'public/**', 'node_modules/**'] },
  ...pluginVue.configs['flat/recommended'],
  ...vueTsEslintConfig(),
  {
    languageOptions: {
      globals: { ...globals.browser },
    },
    rules: {
      // 单文件组件体量可控，暂不强制拆分阈值
      'vue/max-attributes-per-line': 'off',
      'vue/singleline-html-element-content-newline': 'off',
      'vue/html-self-closing': 'off',
      'vue/html-indent': 'off',
      // 模板属性排序交给既有习惯
      'vue/attributes-order': 'off',
    },
  },
  {
    // CodeEditor.vue：CM6 全量走运行时动态 import（独立懒 chunk），模块类型拿不到静态声明，
    // 内部以宽松类型持有扩展实例——语义级补全/类型化封装留待后续按需补
    files: ['src/components/CodeEditor.vue'],
    rules: {
      '@typescript-eslint/no-explicit-any': 'off',
    },
  },
  {
    // 富文本编辑内芯与公式弹层：v-html 是唯一渲染路径——
    // MdEditorInner 渲染 Tiptap schema 自身产出的 HTML（编辑器内容受 schema 约束），
    // MdMathDialog 渲染 KaTeX 输出（mathjax/katex 生成，非用户裸 HTML）；
    // 查看端的用户内容一律走 MdView（DOMPurify 出口消毒），不在此豁免面内
    files: ['src/components/MdEditorInner.vue', 'src/components/MdMathDialog.vue'],
    rules: {
      'vue/no-v-html': 'off',
    },
  },
]
