package server

// 个人设置（v0.4.8）：本人自助字段（性别/学校/年级/QQ）+ 偏好（默认语言/代码模板/MD 编辑模式）。
// 真实姓名仅管理员经 PATCH /admin/users/:id 修改，本人只读；所有字段仅 /me/profile
// 与管理员用户列表返回，不进任何公开响应（userJSON 保持不动）。

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"

	"soj/internal/lang"
	"soj/internal/repo"
)

var qqDigits = regexp.MustCompile(`^\d{5,16}$`)

// trimTo 去首尾空白并按字符数截断（中文按 1 字符计）。
func trimTo(s string, max int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > max {
		r = r[:max]
	}
	return string(r)
}

// profileJSON 本人视角的个人信息 + 偏好。
func profileJSON(u *repo.User) gin.H {
	prefs := u.Prefs
	if prefs == nil {
		prefs = &repo.UserPrefs{}
	}
	if prefs.CodeTemplates == nil {
		prefs.CodeTemplates = map[string]string{}
	}
	return gin.H{
		"real_name": u.RealName,
		"gender":    u.Gender,
		"school":    u.School,
		"grade":     u.Grade,
		"qq":        u.QQ,
		"prefs":     prefs,
	}
}

// GetMyProfile 当前用户的个人信息与偏好（要求登录）。
func (h *Handlers) GetMyProfile(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	u, err := h.repo.GetUserByID(c.Request.Context(), uid)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "用户不存在")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取失败")
		return
	}
	ok(c, profileJSON(u))
}

// UpdateMyProfile 更新本人自助字段与偏好（指针语义：出现才改，显式空串=清空）。
func (h *Handlers) UpdateMyProfile(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	var req struct {
		Gender *string `json:"gender"`
		School *string `json:"school"`
		Grade  *string `json:"grade"`
		QQ     *string `json:"qq"`
		Prefs  *struct {
			DefaultLang   *string            `json:"defaultLang"`
			CodeTemplates *map[string]string `json:"codeTemplates"`
			MdEditMode    *string            `json:"mdEditMode"`
			Autocomplete  *bool              `json:"autocomplete"`
			CodeFont      *string            `json:"codeFont"`
			CodeFontSize  *int               `json:"codeFontSize"`
		} `json:"prefs"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	set := bson.M{}
	bindStr := func(p *string, key string, max int) {
		if p != nil {
			set[key] = trimTo(*p, max)
		}
	}
	bindStr(req.Gender, "gender", 8)
	bindStr(req.School, "school", 64)
	bindStr(req.Grade, "grade", 16)
	if req.QQ != nil {
		qq := trimTo(*req.QQ, 16)
		if qq != "" && !qqDigits.MatchString(qq) {
			fail(c, http.StatusBadRequest, "QQ 需为 5-16 位数字")
			return
		}
		set["qq"] = qq
	}
	if req.Prefs != nil {
		prefs := repo.UserPrefs{}
		if req.Prefs.DefaultLang != nil {
			l := trimTo(*req.Prefs.DefaultLang, 32)
			if l != "" && !lang.Exists(l) {
				fail(c, http.StatusBadRequest, "未知判题语言")
				return
			}
			prefs.DefaultLang = l
		}
		if req.Prefs.MdEditMode != nil {
			m := trimTo(*req.Prefs.MdEditMode, 8)
			switch m {
			case "", "wysiwyg", "ir", "sv":
				prefs.MdEditMode = m
			default:
				fail(c, http.StatusBadRequest, "MD 编辑模式需为 wysiwyg / ir / sv")
				return
			}
		}
		if req.Prefs.CodeTemplates != nil {
			tpl := *req.Prefs.CodeTemplates
			if len(tpl) > 16 {
				fail(c, http.StatusBadRequest, "代码模板语言过多（上限 16）")
				return
			}
			m := make(map[string]string, len(tpl))
			for k, v := range tpl {
				if !lang.Exists(k) {
					fail(c, http.StatusBadRequest, "代码模板语言未知："+k)
					return
				}
				if len(v) > 16<<10 {
					fail(c, http.StatusBadRequest, "单个代码模板过大（上限 16KB）")
					return
				}
				m[k] = v
			}
			prefs.CodeTemplates = m
		}
		if req.Prefs.Autocomplete != nil {
			prefs.Autocomplete = req.Prefs.Autocomplete
		}
		if req.Prefs.CodeFont != nil {
			f := strings.TrimSpace(*req.Prefs.CodeFont)
			switch f {
			case "", "Consolas", "Menlo", "Monaco":
				prefs.CodeFont = f
			default:
				fail(c, http.StatusBadRequest, "字体仅支持 Consolas / Menlo / Monaco")
				return
			}
		}
		if req.Prefs.CodeFontSize != nil {
			n := *req.Prefs.CodeFontSize
			if n != 0 && (n < 12 || n > 28) {
				fail(c, http.StatusBadRequest, "字号需在 12-28 之间")
				return
			}
			prefs.CodeFontSize = n
		}
		set["prefs"] = &prefs
	}
	if len(set) == 0 {
		fail(c, http.StatusBadRequest, "没有需要更新的字段")
		return
	}
	if err := h.repo.UpdateUser(c.Request.Context(), uid, set); err != nil {
		if repo.IsNotFound(err) {
			fail(c, http.StatusNotFound, "用户不存在")
			return
		}
		fail(c, http.StatusInternalServerError, "保存失败")
		return
	}
	u, err := h.repo.GetUserByID(c.Request.Context(), uid)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取失败")
		return
	}
	ok(c, profileJSON(u))
}
