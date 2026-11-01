package server

// 个人设置（v0.4.8）：本人自助字段（昵称/性别/学校/年级/QQ）+ 偏好（默认语言/代码模板）。
// 昵称修改受 user.nickname.edit 权限位控制（默认角色默认开启）；其余字段仅 /me/profile
// 与管理员用户列表返回，不进任何公开响应（userJSON 保持不动）。
// 资料自改开关（v0.34）：后台系统设置逐字段可锁（昵称/性别/年级/学校/地区），锁定的
// 字段 PATCH 拒收、前端只读；editable 五标志随 profile 下发（昵称 = 权限位 AND 开关）。

import (
	"log/slog"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"

	"soj/internal/lang"
	"soj/internal/repo"
)

var qqDigits = regexp.MustCompile(`^\d{5,16}$`)

// qqAvatarURL QQ 头像直链（v0.23）：/me/profile（顶栏/设置页）与公开主页共用一源
//——曾两处手抄漂移致顶栏裂图（v0.34 回归，2026-10-30 修复），收口单点。
func qqAvatarURL(qq string) string {
	return "https://q1.qlogo.cn/g?b=qq&nk=" + qq + "&s=100"
}

// maxNicknameRunes 昵称长度上限（字符数，中文按 1 计；2026-10-27 用户拍板 6 字以内）。
// 本人设置/管理端/注册三处写入口统一走此值，trimTo 静默截断。
const maxNicknameRunes = 6

// trimTo 去首尾空白并按字符数截断（中文按 1 字符计）。
func trimTo(s string, max int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > max {
		r = r[:max]
	}
	return string(r)
}

// profileEditable 当前用户五个资料字段的自改判定：后台逐字段开关（默认全开），
// 昵称再 AND user.nickname.edit 权限位（双闸——保留角色粒度）。
func (h *Handlers) profileEditable(c *gin.Context) repo.ProfileEditConfig {
	cfg := *repo.DefaultProfileEditConfig()
	if s, err := h.repo.GetSystemSettings(c.Request.Context()); err == nil {
		cfg = s.ProfileEditCfg()
	}
	cfg.Nickname = cfg.Nickname && hasPerm(c, repo.PermUserNicknameEdit)
	return cfg
}

// profileJSON 本人视角的个人信息 + 偏好；editable 为五字段自改标志（v0.34），
// 前端据此决定输入框可用状态（不依赖登录时缓存的旧权限/旧设置）。
func profileJSON(u *repo.User, editable repo.ProfileEditConfig) gin.H {
	prefs := u.Prefs
	if prefs == nil {
		prefs = &repo.UserPrefs{}
	}
	if prefs.CodeTemplates == nil {
		prefs.CodeTemplates = map[string]string{}
	}
	vis := u.EffProfileVis()
	info := gin.H{
		"nickname": u.Nickname,
		"editable": gin.H{
			"nickname": editable.Nickname,
			"gender":   editable.Gender,
			"grade":    editable.Grade,
			"school":   editable.School,
			"region":   editable.Region,
		},
		"gender":      u.Gender,
		"school":      u.School,
		"grade":       u.GradeName(), // 年级显示名（v0.34 结构化）；空串 = 未设置
		"region":      u.Region,
		"qq":          u.QQ,
		"profile_vis": vis, // 资料公开开关（v0.23）：设置页开关回填
		"qq_avatar":   u.QQAvatar,
		"prefs":       prefs,
	}
	// QQ 头像（v0.23）：设置页与顶栏头像共用；绑定开关开且已填 QQ 才下发
	if u.QQAvatar && u.QQ != "" {
		info["avatar_url"] = qqAvatarURL(u.QQ)
	}
	return info
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
	info := profileJSON(u, h.profileEditable(c))
	// 首登改密提醒：随 profile 下发——布局层每次挂载/登录后都会拉 profile，前端免增请求
	info["pwdRemind"] = u.PwdRemind
	// 等级（v0.11）：徽章/进度渲染；聚合失败降级省略，不阻塞个人设置
	if lv := h.levelBadgeInfo(c, uid); lv != nil {
		info["level"] = lv
		// 升维祝福（v0.40）：xpAdjust 等非 AC 升级的兜底检测（AC 路径在判题回写）
		if level, _ := lv["level"].(int); level >= 2 {
			if err := h.repo.EnsureLevelBlessing(c.Request.Context(), uid, level); err != nil {
				slog.Warn("升维祝福检测失败", "component", "profile", "uid", uid, "error", err)
			}
		}
	}
	// 游戏感批（v0.41）：连击/坚持现状（首页火焰与个人主页消费）
	if st, err := h.repo.GetGameStats(c.Request.Context(), uid); err == nil {
		info["game"] = gin.H{
			"combo_cur": st.ComboCur, "combo_best": st.ComboBest,
			"streak_days": st.StreakDays, "streak_best": st.StreakBest,
		}
	}
	ok(c, info)
}

// SkipPwdRemind 首登改密提醒·跳过（用户拍板：提醒可跳过，跳过=不再提醒）。
// 关弹窗（未做选择）不清标志，下次登录仍会提醒；改密路径走 UpdatePassword 自动清除。
func (h *Handlers) SkipPwdRemind(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	if err := h.repo.SetPwdRemind(c.Request.Context(), uid, false); err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	ok(c, gin.H{"skipped": true})
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
		Region   *string `json:"region"`
		QQ       *string `json:"qq"`
		// 资料公开开关（v0.23）：整组提交（出现才改，逐字段布尔）
		ProfileVis *repo.ProfileVis `json:"profileVis"`
		QQAvatar   *bool            `json:"qqAvatar"`
		Prefs *struct {
			DefaultLang   *string            `json:"defaultLang"`
			CodeTemplates *map[string]string `json:"codeTemplates"`
			Autocomplete  *bool              `json:"autocomplete"`
			CodeFont      *string            `json:"codeFont"`
			CodeFontSize  *int               `json:"codeFontSize"`
			MdEditMode    *string            `json:"mdEditMode"`
		} `json:"prefs"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	var patch repo.UserPatch
	editable := h.profileEditable(c)
	// 锁定字段拒收（v0.34 资料自改开关）：昵称另有权限位文案，其余统一「管理员已锁定」
	if req.Nickname != nil {
		if !hasPerm(c, repo.PermUserNicknameEdit) {
			fail(c, http.StatusForbidden, "当前角色不允许修改昵称")
			return
		}
		if !editable.Nickname {
			fail(c, http.StatusForbidden, "管理员已锁定昵称的修改")
			return
		}
		nickname := trimTo(*req.Nickname, maxNicknameRunes)
		if nickname == "" {
			fail(c, http.StatusBadRequest, "昵称不能为空")
			return
		}
		patch.Nickname = &nickname
	}
	if req.Gender != nil && !editable.Gender {
		fail(c, http.StatusForbidden, "管理员已锁定性别的修改")
		return
	}
	if req.Grade != nil && !editable.Grade {
		fail(c, http.StatusForbidden, "管理员已锁定年级的修改")
		return
	}
	if req.School != nil && !editable.School {
		fail(c, http.StatusForbidden, "管理员已锁定学校的修改")
		return
	}
	if req.Region != nil && !editable.Region {
		fail(c, http.StatusForbidden, "管理员已锁定地区的修改")
		return
	}
	bindStr := func(p *string, dst **string, max int) {
		if p != nil {
			v := trimTo(*p, max)
			*dst = &v
		}
	}
	bindStr(req.Gender, &patch.Gender, 8)
	bindStr(req.School, &patch.School, 64)
	bindStr(req.Region, &patch.Region, 32)
	if req.Grade != nil {
		// 年级（v0.34 结构化）：API 收显示名/数字（空串 = 清空回未设置），
		// 无法识别的文本拒绝（下拉不该出现，防直连接口写脏数据）
		g := strings.TrimSpace(*req.Grade)
		if no := repo.ParseGrade(g); no > 0 || g == "" {
			patch.GradeNo = &no
		} else {
			fail(c, http.StatusBadRequest, "年级无法识别（支持：一年级…高三 / 初一…初三 / 已毕业 / 数字 1-12）")
			return
		}
	}
	if req.QQ != nil {
		qq := trimTo(*req.QQ, 16)
		if qq != "" && !qqDigits.MatchString(qq) {
			fail(c, http.StatusBadRequest, "QQ 需为 5-16 位数字")
			return
		}
		patch.QQ = &qq
	}
	if req.ProfileVis != nil {
		patch.ProfileVis = req.ProfileVis
	}
	if req.QQAvatar != nil {
		patch.QQAvatar = req.QQAvatar
	}
	if req.Prefs != nil {
		// 合并语义：在既有偏好上覆盖本次出现的字段。编辑器工具栏的自动回存只带
		// mdEditMode 一项，不能把默认语言、代码模板等旧偏好冲掉
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
			case "", "split", "source":
				prefs.MdEditMode = m
			default:
				fail(c, http.StatusBadRequest, "Markdown 编辑模式仅支持 split / source")
				return
			}
		}
		patch.Prefs = &prefs
	}
	if patch.Empty() {
		fail(c, http.StatusBadRequest, "没有需要更新的字段")
		return
	}
	if err := h.repo.UpdateUser(c.Request.Context(), uid, patch); err != nil {
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
	ok(c, profileJSON(u, h.profileEditable(c)))
}
