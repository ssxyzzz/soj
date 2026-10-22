package repo

import (
	"regexp"
	"strconv"
	"strings"
)

// 来源四字段（v0.12）：source_type 枚举固定五类不可扩（空串 = 未归类，来源名仍可单独
// 存在）；source/year/region 均可选。来源体系独立于知识点标签树；组别/轮次并入来源名。
const (
	SourceTypeOJ      = "oj"      // OJ 站点（洛谷 / Codeforces / USACO……）
	SourceTypeContest = "contest" // 全国赛事（NOIP / CSP / 蓝桥杯……）
	SourceTypeRegion  = "region"  // 地区活动（合肥市信息学科普日……）
	SourceTypeLocal   = "local"   // 校内·原创
	SourceTypeOther   = "other"   // 其他
)

// SourceTypes 枚举全集（校验与后台面板用）。
var SourceTypes = []string{SourceTypeOJ, SourceTypeContest, SourceTypeRegion, SourceTypeLocal, SourceTypeOther}

// SourceTypeLabels 类型展示名（详情徽章链 / 后台来源面板 / 表单下拉共用）。
var SourceTypeLabels = map[string]string{
	SourceTypeOJ:      "OJ 站点",
	SourceTypeContest: "全国赛事",
	SourceTypeRegion:  "地区活动",
	SourceTypeLocal:   "校内·原创",
	SourceTypeOther:   "其他",
}

// ValidSourceType 是否合法来源类型（空串 = 未归类，合法）。
func ValidSourceType(t string) bool {
	if t == "" {
		return true
	}
	for _, v := range SourceTypes {
		if v == t {
			return true
		}
	}
	return false
}

// 标题方括号前缀识别（v0.12）：`[USACO 1.3.2] Barn Repair` → 来源 USACO 1.3.2/OJ 站点；
// `[2025瑶海区小学]` / `[2025 瑶海区小学]` → 年份 2025 + 地区 瑶海区 + 来源 小学（组别并入
// 来源名）/地区活动。仅用于回填空缺字段——显式设置的来源永远优先。
var (
	reSourceBracket = regexp.MustCompile(`^\s*[\[［]([^\[\]］]{1,48})[\]］]`)
	reSourceYear    = regexp.MustCompile(`(19|20)\d{2}`)
	reSourceRegion  = regexp.MustCompile(`[\p{Han}]{1,10}(?:省|市|区|县)`)
)

// sourceOJKeywords 已知 OJ 站点名前缀（小写匹配）：命中来源名 → 类型 OJ 站点。
// 用前缀而非子串，避免短词（cf 等）误命中普通题名。
var sourceOJKeywords = []string{
	"usaco", "codeforces", "loj", "hydro", "atcoder", "leetcode", "luogu",
	"spoj", "poj", "hdu", "uva", "bzoj", "codeup", "洛谷", "牛客",
}

// SourceHint 标题识别结果：零值 = 无可识别前缀。
type SourceHint struct {
	SourceType string
	Source     string
	Year       int
	Region     string
}

// ParseSourceHint 从题名解析来源提示：取开头的方括号前缀（中英文括号均可），
// 段内 4 位年份 → Year，「xx省/市/区/县」→ Region（取首个、从段内剔除），余串（去分隔
// 符后非空）→ Source；类型按关键词推断（OJ 名 → oj，有地区 → region，仅年份 → contest，
// 兜底 other）。来源名为空视为无提示。
func ParseSourceHint(title string) SourceHint {
	m := reSourceBracket.FindStringSubmatch(title)
	if m == nil {
		return SourceHint{}
	}
	content := strings.TrimSpace(m[1])

	hint := SourceHint{}
	if ym, ok := matchSourceYear(content); ok {
		hint.Year, _ = strconv.Atoi(ym)
		content = strings.TrimSpace(strings.Replace(content, ym, "", 1))
	}
	if rm := reSourceRegion.FindString(content); rm != "" {
		hint.Region = rm
		content = strings.TrimSpace(strings.Replace(content, rm, "", 1))
	}
	content = strings.Trim(content, " \t·•-_—-、，,。")
	content = strings.Join(strings.Fields(content), " ") // 剥离年份/地区后的多空格归一
	if content == "" && hint.Year == 0 && hint.Region == "" {
		return SourceHint{}
	}
	if content != "" {
		const maxLen = 64
		if len([]rune(content)) > maxLen {
			content = string([]rune(content)[:maxLen])
		}
		hint.Source = content
	}

	switch {
	case hint.Source != "" && inferOJSource(hint.Source):
		hint.SourceType = SourceTypeOJ
	case hint.Region != "":
		hint.SourceType = SourceTypeRegion
	case hint.Year != 0:
		hint.SourceType = SourceTypeContest
	default:
		hint.SourceType = SourceTypeOther
	}
	return hint
}

// inferOJSource 来源名是否以已知 OJ 站点名开头。
func inferOJSource(source string) bool {
	low := strings.ToLower(source)
	for _, kw := range sourceOJKeywords {
		if strings.HasPrefix(low, kw) {
			return true
		}
	}
	return false
}

// matchSourceYear 找段内首个 4 位年份（19xx/20xx），且其后不能紧跟 ASCII 字母/数字——
// `[Codeforces 1900B]` 的 1900 是题号不是年份；`NOIP2020`、`2025瑶海区` 不受影响。
func matchSourceYear(content string) (string, bool) {
	off := 0
	for {
		loc := reSourceYear.FindStringIndex(content[off:])
		if loc == nil {
			return "", false
		}
		absStart, absEnd := off+loc[0], off+loc[1]
		ym := content[absStart:absEnd]
		if absEnd >= len(content) {
			return ym, true // 年份到段尾
		}
		if next := content[absEnd]; next >= 0x80 || !isASCIILetterDigit(next) {
			return ym, true // 其后是汉字/标点 → 年份；紧跟 ASCII 字母数字 → 题号，继续找
		}
		off = absEnd
	}
}

func isASCIILetterDigit(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}
