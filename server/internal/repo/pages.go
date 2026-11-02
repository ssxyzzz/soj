package repo

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"soj/internal/mdrender"
)

// Page 自定义页面（v0.56，pages 集合）：后台直配的「零代码上线」通道——外链（link）、
// 内嵌工具（iframe，drawio 协议壳为画板页）、Markdown 说明页（markdown，写时渲染缓存
// html，查看端不下载渲染引擎）。slug 即 _id 与 /p/:slug 路由参数，建后不可改。
// 可见性：login = 登录即可见；roles = 命中 Roles 清单之一（角色 ID 全量校验，防手误挂空）。
type Page struct {
	Slug        string    `bson:"_id"`
	Title       string    `bson:"title"`
	Type        string    `bson:"type"` // link | iframe | markdown
	URL         string    `bson:"url,omitempty"`         // link/iframe：外链或内嵌地址（http/https）
	Proto       string    `bson:"proto,omitempty"`       // iframe：空 = 普通内嵌；drawio = 画板协议壳
	Content     string    `bson:"content,omitempty"`     // markdown：源（唯一事实源，仅 admin 视图下发）
	ContentHTML string    `bson:"contentHtml,omitempty"` // markdown：写时渲染缓存（v0.53 渲染收口同款）
	Visibility  string    `bson:"visibility"`            // login | roles
	Roles       []string  `bson:"roles,omitempty"`       // visibility=roles：命其一即可见
	NavShow     bool      `bson:"navShow"`               // 导航入口
	FooterShow  bool      `bson:"footerShow"`            // 页脚入口
	Enabled     bool      `bson:"enabled"`
	Sort        int64     `bson:"sort"`
	UpdatedAt   time.Time `bson:"updatedAt,omitempty"`
}

// 类型 / 可见性 / 协议取值与字段上限（保存入口校验，错误信息可直接展示）。
const (
	PageTypeLink     = "link"
	PageTypeIframe   = "iframe"
	PageTypeMarkdown = "markdown"

	PageVisibilityLogin = "login"
	PageVisibilityRoles = "roles"

	PageProtoDrawio = "drawio"

	MaxPageSlugLen    = 32
	MaxPageTitleLen   = 24
	MaxPageURLLen     = 512
	MaxPageContentLen = 50_000 // markdown 源字符上限
	MaxPageRoles      = 8
)

// pageSlugRe slug 规则：小写字母/数字开头，可含连字符，1–32 位（URL 安全段）。
var pageSlugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// ValidPageSlug slug 合法性。
func ValidPageSlug(s string) bool { return pageSlugRe.MatchString(s) }

// ValidPageURL 外链/内嵌地址 scheme 白名单（防 javascript: 等内联 scheme 进 href/iframe src）。
func ValidPageURL(url string) bool {
	return strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://")
}

// NormalizePageRoles 角色清单归一：trim、去空、去重保序。
func NormalizePageRoles(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, r := range in {
		r = strings.TrimSpace(r)
		if r == "" || seen[r] {
			continue
		}
		seen[r] = true
		out = append(out, r)
	}
	return out
}

// PageRolesValid 角色 ID 全部存在（内置 + 自定义角色；下拉候选与保存校验同源）。
func (r *Repo) PageRolesValid(ctx context.Context, ids []string) (bool, error) {
	roles, err := r.ListRoles(ctx)
	if err != nil {
		return false, err
	}
	have := make(map[string]bool, len(roles))
	for _, ro := range roles {
		have[ro.ID] = true
	}
	for _, id := range ids {
		if !have[id] {
			return false, nil
		}
	}
	return true, nil
}

// pageSort 排序键：sort 升序，同序按 slug 稳定。
var pageSort = bson.D{{Key: "sort", Value: 1}, {Key: "_id", Value: 1}}

// AllPages 后台全量清单（含停用项）。
func (r *Repo) AllPages(ctx context.Context) ([]*Page, error) {
	cur, err := r.db.Collection("pages").Find(ctx, bson.M{}, options.Find().SetSort(pageSort))
	if err != nil {
		return nil, err
	}
	var out []*Page
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// VisiblePages 当前角色可见的自定义页面：enabled ∧（login ∨ 命中角色清单）。
func (r *Repo) VisiblePages(ctx context.Context, role string) ([]*Page, error) {
	filter := bson.M{
		"enabled": true,
		"$or": []bson.M{
			{"visibility": PageVisibilityLogin},
			{"visibility": PageVisibilityRoles, "roles": role},
		},
	}
	cur, err := r.db.Collection("pages").Find(ctx, filter, options.Find().SetSort(pageSort))
	if err != nil {
		return nil, err
	}
	var out []*Page
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetPage 按 slug 读单页（IsNotFound 可判）。
func (r *Repo) GetPage(ctx context.Context, slug string) (*Page, error) {
	var p Page
	err := r.db.Collection("pages").FindOne(ctx, bson.M{"_id": slug}).Decode(&p)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// CreatePage 新建（slug 冲突回 ErrPageSlugTaken）。
func (r *Repo) CreatePage(ctx context.Context, p *Page) error {
	p.UpdatedAt = time.Now()
	if _, err := r.db.Collection("pages").InsertOne(ctx, p); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return fmt.Errorf("slug 已被占用")
		}
		return err
	}
	return nil
}

// pageFieldSet 更新字段集（slug 即 _id 不在列——建后不可改）。
func pageFieldSet(p *Page) bson.M {
	return bson.M{
		"title":       p.Title,
		"type":        p.Type,
		"url":         p.URL,
		"proto":       p.Proto,
		"content":     p.Content,
		"contentHtml": p.ContentHTML,
		"visibility":  p.Visibility,
		"roles":       NormalizePageRoles(p.Roles),
		"navShow":     p.NavShow,
		"footerShow":  p.FooterShow,
		"enabled":     p.Enabled,
		"sort":        p.Sort,
		"updatedAt":   time.Now(),
	}
}

// UpdatePage 整体更新（slug 不变）。
func (r *Repo) UpdatePage(ctx context.Context, slug string, p *Page) error {
	res, err := r.db.Collection("pages").UpdateOne(ctx, bson.M{"_id": slug}, bson.M{"$set": pageFieldSet(p)})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// DeletePage 按 slug 删。
func (r *Repo) DeletePage(ctx context.Context, slug string) error {
	res, err := r.db.Collection("pages").DeleteOne(ctx, bson.M{"_id": slug})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// RenderPageContent markdown 写时渲染（创建/更新统一走这里，html 只增不改源事实）。
func RenderPageContent(p *Page) {
	p.ContentHTML = mdrender.Render(p.Content)
}
