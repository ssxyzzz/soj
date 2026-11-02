// v0.57 样式门禁（plan §2.4）：.vue 里禁裸 hex 与裸字号/字重，强制走
// src/styles/main.css :root 的 --soj-* token（唯一改值入口）。
// 白名单（overrides 顺序：通用规则在前、豁免在后，后者覆盖前者）：
//   - src/styles/** —— token 底座本体（:root 定值与全局 reskin 只许出现在这里）
//   - src/components/game/** —— 游戏皮肤定值（自包含美术资产，主题不随动）；
//     字号/字重仍走 token
//   - 局部皮肤段用 /* stylelint-disable color-no-hex -- 理由 */ 包裹（编辑器主题等）
// 间距暂不设 lint：--soj-sp-* 档位靠规范约束，观察一批后再决定是否上强规则。
export default {
  // 全部规则经 overrides 按文件域生效；stylelint 要求顶层必须有 rules 键
  rules: {},
  overrides: [
    {
      files: ['src/**/*.vue'],
      customSyntax: 'postcss-html',
      rules: {
        'color-no-hex': true,
        'declaration-property-value-allowed-list': {
          // 字号：--soj-fs-* 语义/基础档；≥40px 装饰图形档（大数字/emoji/游戏美术）豁免
          'font-size': ['/^var\\(--soj-fs-[a-z0-9-]+\\)$/', '/^calc\\(var\\(--soj-fs-.*\\)$/', '/^([4-9][0-9]|1[0-9][0-9])px$/'],
          // 字重：--soj-fw-* 五档（400/500/600/700/800；900 已归并 heavy）
          'font-weight': ['/^var\\(--soj-fw-[a-z-]+\\)$/', 'normal', 'bold', 'inherit'],
        },
      },
    },
    {
      files: ['src/**/*.css'],
      rules: {
        'color-no-hex': true,
        'declaration-property-value-allowed-list': {
          'font-size': ['/^var\\(--soj-fs-[a-z0-9-]+\\)$/', '/^calc\\(var\\(--soj-fs-.*\\)$/', '/^([4-9][0-9]|1[0-9][0-9])px$/'],
          'font-weight': ['/^var\\(--soj-fw-[a-z-]+\\)$/', 'normal', 'bold', 'inherit'],
        },
      },
    },
    {
      // token 底座与全局 reskin：定值与派生只许写在这里
      files: ['src/styles/**/*.css'],
      rules: {
        'color-no-hex': null,
        'declaration-property-value-allowed-list': null,
      },
    },
    {
      // 游戏皮肤定值豁免（FeastOverlay 等金棕系美术色板）
      files: ['src/components/game/**/*.vue'],
      customSyntax: 'postcss-html',
      rules: {
        'color-no-hex': null,
      },
    },
  ],
}
