// CM6 搜索面板中文定制（v0.82.6）：basicSetup 内置 searchKeymap 的默认面板是英文文案 +
// UA 裸样式（search / next / previous / all / match case / regexp / by word / Replace /
// replace / replace all，用户截图直拍要求中文化 + 字体对齐站点）。本扩展以 search() 的
// createPanel 覆盖默认面板实现：文案全中文、控件继承站点字体（正文用 --el-font-family
// CJK 先行栈，查找/替换输入框用 --font-mono 与编辑器内容一致），功能与默认面板一一对应。
// 接入点：MdEditorInner（题面）/ CodeEditor（代码）/ DiffView（对比）三编辑器共用本扩展。
import {
  search,
  SearchQuery,
  setSearchQuery,
  getSearchQuery,
  findNext,
  findPrevious,
  replaceNext,
  replaceAll,
  selectMatches,
  closeSearchPanel,
} from '@codemirror/search'
import { EditorView } from '@codemirror/view'
import type { Panel, ViewUpdate } from '@codemirror/view'

// 站点 UI 字体（与 main.css --el-font-family 同栈：CJK 先行纪律，勿把拉丁提前）
const UI_FONT =
  "'Noto Sans SC', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', '微软雅黑', sans-serif"

export function searchZh() {
  return [
    EditorView.theme({
      '.cm-panel.cm-searchZh': {
        fontFamily: UI_FONT,
        fontSize: '13px',
        padding: '8px 12px',
        display: 'flex',
        flexDirection: 'column',
        gap: '6px',
        backgroundColor: 'var(--soj-panel, #fff)',
        borderTop: '1px solid var(--soj-line, rgba(11, 31, 69, 0.08))',
        alignItems: 'flex-start',
      },
      '.cm-panel.cm-searchZh .szh-row': {
        display: 'flex',
        alignItems: 'center',
        gap: '6px',
        flexWrap: 'wrap',
      },
      '.cm-panel.cm-searchZh input[type="text"]': {
        fontFamily: 'var(--font-mono, Consolas, monospace)',
        fontSize: '13px',
        padding: '4px 8px',
        border: '1px solid var(--soj-line, rgba(11, 31, 69, 0.08))',
        borderRadius: '6px',
        outline: 'none',
        minWidth: '140px',
        background: 'var(--soj-bg, #fff)',
        color: 'var(--soj-ink-1, #0b1f45)',
      },
      '.cm-panel.cm-searchZh input[type="text"]:focus': {
        borderColor: 'var(--soj-blue, #007bff)',
      },
      '.cm-panel.cm-searchZh .szh-btn': {
        fontFamily: UI_FONT,
        fontSize: '12px',
        lineHeight: 1,
        padding: '6px 10px',
        border: '1px solid var(--soj-line, rgba(11, 31, 69, 0.08))',
        borderRadius: '6px',
        background: 'var(--soj-panel, #fff)',
        color: 'var(--soj-ink-2, #48607f)',
        cursor: 'pointer',
      },
      '.cm-panel.cm-searchZh .szh-btn:hover': {
        borderColor: 'var(--soj-blue, #007bff)',
        color: 'var(--soj-blue, #007bff)',
      },
      '.cm-panel.cm-searchZh label': {
        fontFamily: UI_FONT,
        fontSize: '12px',
        color: 'var(--soj-ink-2, #48607f)',
        display: 'inline-flex',
        alignItems: 'center',
        gap: '3px',
        cursor: 'pointer',
        userSelect: 'none',
      },
      '.cm-panel.cm-searchZh .szh-close': {
        fontFamily: UI_FONT,
        fontSize: '16px',
        lineHeight: 1,
        marginLeft: 'auto',
        border: 'none',
        background: 'transparent',
        color: 'var(--soj-ink-3, #8da2bc)',
        cursor: 'pointer',
        padding: '2px 6px',
      },
      '.cm-panel.cm-searchZh .szh-close:hover': {
        color: 'var(--soj-ink-1, #0b1f45)',
      },
    }),
    search({
      createPanel(view: EditorView): Panel {
        const root = document.createElement('div')
        root.className = 'cm-searchZh'

        const row1 = document.createElement('div')
        row1.className = 'szh-row'
        const row2 = document.createElement('div')
        row2.className = 'szh-row'
        root.append(row1, row2)

        // ---- 第一行：查找 + 翻转 + 全部 + 三个开关 + 关闭 ----
        const searchInput = document.createElement('input')
        searchInput.type = 'text'
        searchInput.placeholder = '查找'
        searchInput.setAttribute('main-field', 'true')

        const mkBtn = (text: string, title: string, run: () => void) => {
          const b = document.createElement('button')
          b.type = 'button'
          b.textContent = text
          b.title = title
          b.className = 'szh-btn'
          b.addEventListener('click', run)
          return b
        }
        const mkCheck = (text: string, title: string) => {
          const label = document.createElement('label')
          const cb = document.createElement('input')
          cb.type = 'checkbox'
          label.append(cb, document.createTextNode(text))
          label.title = title
          return { label, cb }
        }

        const prevBtn = mkBtn('上一处', '上一个匹配（Shift+Enter）', () => findPrevious(view))
        const nextBtn = mkBtn('下一处', '下一个匹配（Enter）', () => findNext(view))
        const allBtn = mkBtn('全部', '选中所有匹配', () => selectMatches(view))
        const caseCk = mkCheck('区分大小写', '区分大小写')
        const reCk = mkCheck('正则', '用正则表达式查找')
        const wordCk = mkCheck('整词', '仅匹配整个单词')

        const closeBtn = document.createElement('button')
        closeBtn.type = 'button'
        closeBtn.className = 'szh-close'
        closeBtn.textContent = '×'
        closeBtn.title = '关闭（Esc）'
        closeBtn.addEventListener('click', () => closeSearchPanel(view))

        row1.append(
          searchInput,
          prevBtn,
          nextBtn,
          allBtn,
          caseCk.label,
          reCk.label,
          wordCk.label,
          closeBtn,
        )

        // ---- 第二行：替换 + 替换 / 替换全部 ----
        const replaceInput = document.createElement('input')
        replaceInput.type = 'text'
        replaceInput.placeholder = '替换为'
        const repBtn = mkBtn('替换', '替换当前匹配并跳到下一个', () => replaceNext(view))
        const repAllBtn = mkBtn('替换全部', '替换所有匹配', () => replaceAll(view))
        row2.append(replaceInput, repBtn, repAllBtn)

        // ---- 查询状态读写 ----
        // 本地最近一次已知 query：update 里用 .eq 对比，等才回写 DOM（防打断输入法/光标）
        let query = getSearchQuery(view.state)
        const syncInputs = () => {
          if (searchInput.value !== query.search) searchInput.value = query.search
          if (replaceInput.value !== query.replace) replaceInput.value = query.replace
          caseCk.cb.checked = query.caseSensitive
          reCk.cb.checked = query.regexp
          wordCk.cb.checked = query.wholeWord
        }
        syncInputs()

        const applyInput = (field: 'search' | 'replace', value: string) => {
          const next =
            field === 'search'
              ? new SearchQuery({
                  search: value,
                  replace: query.replace,
                  caseSensitive: query.caseSensitive,
                  regexp: query.regexp,
                  wholeWord: query.wholeWord,
                })
              : new SearchQuery({
                  search: query.search,
                  replace: value,
                  caseSensitive: query.caseSensitive,
                  regexp: query.regexp,
                  wholeWord: query.wholeWord,
                })
          query = next
          view.dispatch({ effects: setSearchQuery.of(next) })
        }
        const toggleFlag = (key: 'caseSensitive' | 'regexp' | 'wholeWords') => {
          query = new SearchQuery({
            search: query.search,
            replace: query.replace,
            caseSensitive: key === 'caseSensitive' ? !query.caseSensitive : query.caseSensitive,
            regexp: key === 'regexp' ? !query.regexp : query.regexp,
            wholeWord: key === 'wholeWords' ? !query.wholeWord : query.wholeWord,
          })
          view.dispatch({ effects: setSearchQuery.of(query) })
        }

        searchInput.addEventListener('input', () => applyInput('search', searchInput.value))
        replaceInput.addEventListener('input', () => applyInput('replace', replaceInput.value))
        caseCk.cb.addEventListener('change', () => toggleFlag('caseSensitive'))
        reCk.cb.addEventListener('change', () => toggleFlag('regexp'))
        wordCk.cb.addEventListener('change', () => toggleFlag('wholeWords'))
        // Enter 查找 / Shift+Enter 回溯 / 替换框 Enter 替换；Esc 关闭（与默认面板同款键位）
        for (const [el, onEnter, onShiftEnter] of [
          [searchInput, findNext, findPrevious] as const,
          [replaceInput, replaceNext, replaceNext] as const,
        ]) {
          el.addEventListener('keydown', (ev) => {
            if (ev.key === 'Enter') {
              ev.preventDefault()
              ;(ev.shiftKey ? onShiftEnter : onEnter)(view)
            } else if (ev.key === 'Escape') {
              ev.preventDefault()
              closeSearchPanel(view)
            }
          })
        }

        return {
          dom: root,
          update(u: ViewUpdate) {
            const q = getSearchQuery(u.state)
            if (!q.eq(query)) {
              query = q // 外部改动（如替换命令后的状态回流）才回写 DOM
              syncInputs()
            }
          },
        }
      },
    }),
  ]
}
