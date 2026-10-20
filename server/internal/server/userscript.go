package server

// 油猴脚本生成（v0.6.1 增强，plan.md §3 备忘项提前落地）：
// GET /api/import/userscript 返回 UserScript 文本，部署域名在下载时按请求 Host
// 烘焙进脚本（零配置）。脚本本体零清洗零上传：只在源站页面把题面富文本（图片绝对化）
// 复制进剪贴板，用户回 SOJ「粘贴导入」走既有清洗管道（KaTeX/MathJax → data-latex
// 契约只在前端一处）。用户浏览器自带登录态与人类指纹——登录墙/反爬站零封号风险。

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

const userscriptTmpl = `// ==UserScript==
// @name         SOJ 题面复制（%s）
// @namespace    soj-import
// @version      1.0.0
// @description  在任意 OJ 题目页，通过油猴菜单「复制题面到 SOJ 剪贴板」，回 SOJ 建题/编辑页的「粘贴导入」即可回填题面。仅在本浏览器内复制，不产生任何自动化请求。
// @match        *://*/*
// @exclude      %s/*
// @grant        GM_setClipboard
// @grant        GM_registerMenuCommand
// @run-at       document-idle
// @noframes
// ==/UserScript==

(function () {
  'use strict';

  // 已知 OJ 的题面容器候选（按命中优先级）；都未命中时逐级回退到 main/#app/body
  var CANDIDATES = [
    '.problem-content',            // Hydro 系
    'div.problem-statement',       // Codeforces
    '[class*="problem-content"]',
    '.markdown-body',              // 通用渲染容器
    '.container [class*="markdown"]',
    'main .container',
    'main',
    '#app main',
    '#app',
    'body'
  ];

  function pickContainer() {
    for (var i = 0; i < CANDIDATES.length; i++) {
      var els = document.querySelectorAll(CANDIDATES[i]);
      for (var j = 0; j < els.length; j++) {
        // 容器要足够"像题面"：至少 200 字符文本，避免命中空壳/页脚
        if ((els[j].innerText || '').trim().length >= 200) return els[j];
      }
    }
    return document.body;
  }

  function absolutize(clone) {
    // 相对地址复制出页面后就失效：img/a 改写为浏览器已解析的绝对地址
    var imgs = clone.querySelectorAll('img');
    for (var i = 0; i < imgs.length; i++) {
      if (imgs[i].getAttribute('src')) imgs[i].setAttribute('src', imgs[i].src);
    }
    var links = clone.querySelectorAll('a[href]');
    for (var k = 0; k < links.length; k++) {
      links[k].setAttribute('href', links[k].href);
    }
    // 脚本/样式/表单残骸不进剪贴板
    var junk = clone.querySelectorAll('script,style,noscript,iframe,form,button,input,select,textarea');
    for (var m = 0; m < junk.length; m++) junk[m].remove();
    return clone;
  }

  function toast(msg, ok) {
    var d = document.createElement('div');
    d.textContent = msg;
    d.style.cssText = 'position:fixed;top:16px;right:16px;z-index:2147483647;padding:10px 16px;' +
      'border-radius:8px;color:#fff;font-size:14px;box-shadow:0 4px 12px rgba(0,0,0,.25);' +
      'background:' + (ok ? '#22c55e' : '#ef4444');
    document.body.appendChild(d);
    setTimeout(function () { d.remove(); }, 2600);
  }

  function copyHTML(html) {
    // 优先 Clipboard API 写 text/html（Chromium/Firefox 支持；菜单点击视为用户激活）
    if (navigator.clipboard && window.ClipboardItem) {
      var blob = new Blob([html], { type: 'text/html' });
      navigator.clipboard.write([new ClipboardItem({ 'text/html': blob })]).then(function () {
        toast('题面已复制，回 SOJ「粘贴导入」即可', true);
      }, function () {
        GM_setClipboard(html, 'html');
        toast('题面已复制（油猴通道）', true);
      });
      return;
    }
    GM_setClipboard(html, 'html');
    toast('题面已复制（油猴通道）', true);
  }

  GM_registerMenuCommand('复制题面到 SOJ 剪贴板', function () {
    try {
      var html = absolutize(pickContainer().cloneNode(true)).innerHTML;
      if (!html || html.length < 200) {
        toast('没有抓到足够的题面内容，请确认当前是题目页', false);
        return;
      }
      copyHTML(html);
    } catch (e) {
      toast('复制失败：' + e.message, false);
    }
  });
})();
`

func (h *Handlers) ImportUserscript(c *gin.Context) {
	scheme := "http"
	if p := c.GetHeader("X-Forwarded-Proto"); p == "https" {
		scheme = "https"
	}
	base := scheme + "://" + c.Request.Host
	c.Header("Content-Type", "application/javascript; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="soj-import.user.js"`)
	c.String(http.StatusOK, fmt.Sprintf(userscriptTmpl, base, base))
}
