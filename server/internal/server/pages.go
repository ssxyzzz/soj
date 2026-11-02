package server

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

// ---- 自定义页面（v0.56）：后台直配的零代码小页（外链/内嵌/Markdown），画板为首个内嵌实例 ----

// pagePublicShape 查看端瘦身：markdown 只发 html（v0.53 渲染收口——源不出 admin 视图）。
func pagePublicShape(p *repo.Page) gin.H {
	return gin.H{
		"slug":         p.Slug,
		"title":        p.Title,
		"type":         p.Type,
		"url":          p.URL,
		"proto":        p.Proto,
		"content_html": p.ContentHTML,
		"nav_show":     p.NavShow,
		"footer_show":  p.FooterShow,
	}
}

// ListVisiblePages 当前用户可见的自定义页面（登录即可用；匿名无自定义入口）。
func (h *Handlers) ListVisiblePages(c *gin.Context) {
	_, _, role, logged := currentUser(c)
	if !logged {
		ok(c, gin.H{"pages": []gin.H{}})
		return
	}
	pages, err := h.repo.VisiblePages(c.Request.Context(), role)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取页面清单失败")
		return
	}
	out := make([]gin.H, 0, len(pages))
	for _, p := range pages {
		out = append(out, pagePublicShape(p))
	}
	ok(c, gin.H{"pages": out})
}

// AdminListPages 后台全量清单 + 角色候选（可见性下拉用；admin.settings 持有者未必能读
// /admin/roles——用户管理权限位，此处随页清单一并下发）。
func (h *Handlers) AdminListPages(c *gin.Context) {
	pages, err := h.repo.AllPages(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取页面清单失败")
		return
	}
	roles, err := h.repo.ListRoles(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取角色清单失败")
		return
	}
	roleOut := make([]gin.H, 0, len(roles))
	for _, r := range roles {
		roleOut = append(roleOut, gin.H{"id": r.ID, "name": r.Name})
	}
	out := make([]gin.H, 0, len(pages))
	for _, p := range pages {
		out = append(out, gin.H{
			"slug": p.Slug, "title": p.Title, "type": p.Type,
			"url": p.URL, "proto": p.Proto,
			"content":   p.Content, // admin 视图回显源
			"visibility": p.Visibility,
			"roles":     p.Roles,
			"nav_show":  p.NavShow,
			"footer_show": p.FooterShow,
			"enabled":   p.Enabled,
			"sort":      p.Sort,
		})
	}
	ok(c, gin.H{"pages": out, "roles": roleOut})
}

// pagePayload 解析并校验创建/更新入参（错误直接 fail 返回）。
func (h *Handlers) pagePayload(c *gin.Context, create bool) *repo.Page {
	var req struct {
		Slug       string   `json:"slug"`
		Title      string   `json:"title"`
		Type       string   `json:"type"`
		URL        string   `json:"url"`
		Proto      string   `json:"proto"`
		Content    string   `json:"content"`
		Visibility string   `json:"visibility"`
		Roles      []string `json:"roles"`
		NavShow    bool     `json:"nav_show"`
		FooterShow bool     `json:"footer_show"`
		Enabled    bool     `json:"enabled"`
		Sort       int64    `json:"sort"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return nil
	}
	p := &repo.Page{
		Slug:       strings.ToLower(strings.TrimSpace(req.Slug)),
		Title:      strings.TrimSpace(req.Title),
		Type:       req.Type,
		URL:        strings.TrimSpace(req.URL),
		Proto:      req.Proto,
		Content:    req.Content,
		Visibility: req.Visibility,
		Roles:      repo.NormalizePageRoles(req.Roles),
		NavShow:    req.NavShow,
		FooterShow: req.FooterShow,
		Enabled:    req.Enabled,
		Sort:       req.Sort,
	}
	if create {
		if !repo.ValidPageSlug(p.Slug) {
			fail(c, http.StatusBadRequest, "slug 需为小写字母/数字开头，仅含小写字母、数字与连字符（1–32 位）")
			return nil
		}
	}
	if runes := []rune(p.Title); len(runes) < 1 || len(runes) > repo.MaxPageTitleLen {
		fail(c, http.StatusBadRequest, "标题需 1–24 字")
		return nil
	}
	switch p.Type {
	case repo.PageTypeLink, repo.PageTypeIframe:
		p.Content, p.ContentHTML = "", ""
		if p.URL == "" {
			fail(c, http.StatusBadRequest, "该类型需填写地址")
			return nil
		}
		if len(p.URL) > repo.MaxPageURLLen || !repo.ValidPageURL(p.URL) {
			fail(c, http.StatusBadRequest, "地址仅支持 http/https 且不超过 512 字符")
			return nil
		}
	case repo.PageTypeMarkdown:
		p.URL, p.Proto = "", ""
		if strings.TrimSpace(p.Content) == "" {
			fail(c, http.StatusBadRequest, "内容不能为空")
			return nil
		}
		if runes := []rune(p.Content); len(runes) > repo.MaxPageContentLen {
			fail(c, http.StatusBadRequest, "内容过长（上限 50000 字）")
			return nil
		}
		repo.RenderPageContent(p) // 写时渲染（v0.53 渲染收口同款：源是唯一事实源）
	default:
		fail(c, http.StatusBadRequest, "类型错误")
		return nil
	}
	if p.Proto != "" && p.Proto != repo.PageProtoDrawio {
		fail(c, http.StatusBadRequest, "协议取值错误")
		return nil
	}
	if p.Type != repo.PageTypeIframe {
		p.Proto = ""
	}
	switch p.Visibility {
	case repo.PageVisibilityLogin:
		p.Roles = nil
	case repo.PageVisibilityRoles:
		if len(p.Roles) == 0 {
			fail(c, http.StatusBadRequest, "指定角色可见时需至少勾选一个角色")
			return nil
		}
		if len(p.Roles) > repo.MaxPageRoles {
			fail(c, http.StatusBadRequest, "角色最多 8 个")
			return nil
		}
		valid, err := h.repo.PageRolesValid(c.Request.Context(), p.Roles)
		if err != nil {
			fail(c, http.StatusInternalServerError, "校验角色失败")
			return nil
		}
		if !valid {
			fail(c, http.StatusBadRequest, "存在无效角色（请重新打开编辑后重选）")
			return nil
		}
	default:
		fail(c, http.StatusBadRequest, "可见性取值错误")
		return nil
	}
	return p
}

// AdminCreatePage 新建自定义页面。
func (h *Handlers) AdminCreatePage(c *gin.Context) {
	p := h.pagePayload(c, true)
	if p == nil {
		return
	}
	if _, err := h.repo.GetPage(c.Request.Context(), p.Slug); err == nil {
		fail(c, http.StatusBadRequest, "slug 已被占用")
		return
	}
	if err := h.repo.CreatePage(c.Request.Context(), p); err != nil {
		fail(c, http.StatusInternalServerError, "创建失败")
		return
	}
	ok(c, gin.H{"slug": p.Slug})
}

// AdminUpdatePage 更新（slug 不变，body 内 slug 忽略）。
func (h *Handlers) AdminUpdatePage(c *gin.Context) {
	slug := c.Param("slug")
	p := h.pagePayload(c, false)
	if p == nil {
		return
	}
	if err := h.repo.UpdatePage(c.Request.Context(), slug, p); err != nil {
		fail(c, http.StatusInternalServerError, "保存失败")
		return
	}
	ok(c, gin.H{"slug": slug})
}

// AdminDeletePage 删除自定义页面。
func (h *Handlers) AdminDeletePage(c *gin.Context) {
	if err := h.repo.DeletePage(c.Request.Context(), c.Param("slug")); err != nil {
		fail(c, http.StatusInternalServerError, "删除失败")
		return
	}
	ok(c, gin.H{"deleted": true})
}
