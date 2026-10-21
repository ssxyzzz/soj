package server

// 个人设置（v0.4.8）：本人自助字段（昵称/性别/学校/年级/QQ）+ 偏好（默认语言/代码模板）。
// 昵称修改受 user.nickname.edit 权限位控制（默认角色默认开启）；其余字段仅 /me/profile
// 与管理员用户列表返回，不进任何公开响应（userJSON 保持不动）。

import (
	"fmt"
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

// profileJSON 本人视角的个人信息 + 偏好；canEditNickname 由当前生效权限位现算，
// 前端据此决定昵称输入框可用状态（不依赖登录时缓存的旧权限）。
func profileJSON(u *repo.User, canEditNickname bool) gin.H {
	prefs := u.Prefs
	if prefs == nil {
		prefs = &repo.UserPrefs{}
	}
	if prefs.CodeTemplates == nil {
		prefs.CodeTemplates = map[string]string{}
	}
	return gin.H{
		"nickname":          u.Nickname,
		"can_edit_nickname": canEditNickname,
		"gender":            u.Gender,
		"school":            u.School,
		"grade":             u.Grade,
		"qq":                u.QQ,
		"prefs":             prefs,
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
	info := profileJSON(u, hasPerm(c, repo.PermUserNicknameEdit))
	// 等级（v0.11）：徽章/进度渲染；聚合失败降级省略，不阻塞个人设置
	if lv := h.levelBadgeInfo(c, uid); lv != nil {
		info["level"] = lv
	}
	ok(c, info)
}

// UpdateMyProfile 更新本人自助字段与偏好（指针语义：出现才改，显式空串=清空）。
func (h *Handlers) UpdateMyProfile(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	var req struct {
		Nickname *string `json:"nickname"`
		Gender   *string `json:"gender"`
		School   *string `json:"school"`
		Grade    *string `json:"grade"`
		QQ       *string `json:"qq"`
		Prefs    *struct {
			DefaultLang   *string            `json:"defaultLang"`
			CodeTemplates *map[string]string `json:"codeTemplates"`
			Autocomplete  *bool              `json:"autocomplete"`
			CodeFont      *string            `json:"codeFont"`
			CodeFontSize  *int               `json:"codeFontSize"`
			MdEditMode    *string            `json:"mdEditMode"`
			MdTheme       *string            `json:"mdTheme"`
		} `json:"prefs"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	set := bson.M{}
	if req.Nickname != nil {
		if !hasPerm(c, repo.PermUserNicknameEdit) {
			fail(c, http.StatusForbidden, "当前角色不允许修改昵称")
			return
		}
		nickname := trimTo(*req.Nickname, 32)
		if nickname == "" {
			fail(c, http.StatusBadRequest, "昵称不能为空")
			return
		}
		set["nickname"] = nickname
	}
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
		// 合并语义：在既有偏好上覆盖本次出现的字段。编辑器工具栏的自动回存只带
		// mdEditMode / mdTheme 一项，不能把默认语言、代码模板等旧偏好冲掉
		prefs := repo.UserPrefs{}
		if u, err := h.repo.GetUserByID(c.Request.Context(), uid); err == nil && u.Prefs != nil {
			prefs = *u.Prefs
		}
		if req.Prefs.DefaultLang != nil {
			l := trimTo(*req.Prefs.DefaultLang, 32)
			if l != "" && !lang.Exists(l) {
				fail(c, http.StatusBadRequest, "未知判题语言")
				return
			}
			prefs.DefaultLang = l
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
			// 等级门槛（v0.11）：开启需等级 ≥ 系统设置阈值（默认 Lv.5）；关闭不受限
			if *req.Prefs.Autocomplete && !h.autocompleteAllowed(c, uid) {
				min := repo.DefaultAutocompleteMinLevel
				if s, err := h.repo.GetSystemSettings(c.Request.Context()); err == nil {
					min = s.MinAutocompleteLevel()
				}
				fail(c, http.StatusBadRequest, fmt.Sprintf("等级达到 Lv.%d 后才能开启自动补全", min))
				return
			}
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
			if n != 0 && (n < 12 || n > 48) {
				fail(c, http.StatusBadRequest, "字号需在 12-48 之间")
				return
			}
			prefs.CodeFontSize = n
		}
		if req.Prefs.MdEditMode != nil {
			m := strings.TrimSpace(*req.Prefs.MdEditMode)
			switch m {
			case "", "wysiwyg", "ir", "sv":
				prefs.MdEditMode = m
			default:
				fail(c, http.StatusBadRequest, "Markdown 编辑模式仅支持 wysiwyg / ir / sv")
				return
			}
		}
		if req.Prefs.MdTheme != nil {
			t := strings.TrimSpace(*req.Prefs.MdTheme)
			switch t {
			case "", "light", "ant-design", "dark", "wechat":
				prefs.MdTheme = t
			default:
				fail(c, http.StatusBadRequest, "Markdown 主题仅支持 light / ant-design / dark / wechat")
				return
			}
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
	ok(c, profileJSON(u, hasPerm(c, repo.PermUserNicknameEdit)))
}
