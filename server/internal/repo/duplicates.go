package repo

// 重复题扫描（v0.18.1 立项；v0.47 换血相似度判重）：标题+题面归一化合并后按
// 「字符 5-gram Jaccard 相似度」找疑似重复组——MinHash 64 行 + LSH 16 带×4 行出候选，
// 签名一致率 ≥ dupSimThreshold 验票，并查集聚类成组。只列疑似重复供管理员自行处置
// （面板一键标重复/编辑/隐藏），不做任何自动删除。
// 内存与题面长度脱钩（v0.25 流式口径延续）：逐题流式更新定长签名，不落 shingle 全集
// ——万题规模签名+元数据仅几 MB。完全重复（原标题+题面逐字相同）签名全等、一致率
// 100% 必然命中，向后兼容；归一化后全空（纯标点标题+空题面）的题彼此视为同壳
//（旧口径「同标题+空题面计入」的推广——归一化后无可辨识内容即同壳）。

import (
	"context"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// 判重参数（常量口径，v0.47）。
const (
	dupSimThreshold = 0.80 // 签名一致率阈值（≈Jaccard 0.8，64 行标准误 ~5%）
	dupSigRows      = 64   // MinHash 行数（一致率 = 命中行数 / 64）
	dupLSHBands     = 16   // LSH 分带数（× 每带 4 行 = 64 行全用）
	dupLSHRows      = 4
	dupGram         = 5  // 字符 n-gram 窗口（中文长 gram，压样板共现误撞）
	dupMaxBucket    = 100 // 单桶超员护栏：大桶多为全库共用样板文本，只保留签名全等对
)

// DuplicateProblemGroup 一组疑似重复题（归一化相似度 ≥ 阈值聚类成组）。
type DuplicateProblemGroup struct {
	Title      string        `json:"title"`
	Preview    string        `json:"preview"`    // 组内首条题面前 80 字（列表辨识用）
	Similarity float64       `json:"similarity"` // 组内最低相似度 0–1（v0.47）
	Problems   []ProblemInfo `json:"problems"`
}

// ProblemInfo 组内单题摘要（列表展示；面板内一键标重复，处理入口另有编辑页链接）。
type ProblemInfo struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Type      string `json:"type"`
	Hidden    bool   `json:"hidden"`
	Duplicate bool   `json:"duplicate"` // v0.47：已标重复
	CreatedBy int64  `json:"created_by"`
	CreatedAt string `json:"created_at"` // RFC3339（前端自行格式化）
}

// normalizeDupText 判重归一化：仅保留字母/数字（Unicode 语义，含 CJK）并小写化，
// 空白与标点全弃——「标题/题面排版微调」不影响相似度（不做全半角折叠）。
func normalizeDupText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// fnv32a 32 位 FNV-1a（内联实现，gram 级调用避免分配）。
func fnv32a(data []byte) uint32 {
	h := uint32(2166136261)
	for _, c := range data {
		h ^= uint32(c)
		h *= 16777619
	}
	return h
}

// dupRowMix 第 row 行的独立扰频：基哈希加行常数后乘法散列折叠——常量定点，
// 扫描结果跨重启稳定（便于对照排查）。
func dupRowMix(h uint32, row int) uint32 {
	x := (uint64(h) + uint64(row)*0x9E3779B97F4A7C15) * 0xFF51AFD7ED558CCD
	x ^= x >> 29
	x *= 0xC4CEB9FE1A85EC53
	x ^= x >> 32
	return uint32(x)
}

// dupSigFromRunes 归一化文本 → MinHash 签名（字符 dupGram-gram，fnv1a32 基哈希，
// 流式逐 gram 更新，无集合落内存）。不足一个窗口的短文本整串为一个 gram；
// 空文本签名恒为全 MaxUint32（全空壳题彼此同签名、一致率 100%）。
func dupSigFromRunes(rs []rune) [dupSigRows]uint32 {
	var sig [dupSigRows]uint32
	for i := range sig {
		sig[i] = ^uint32(0)
	}
	if len(rs) == 0 {
		return sig
	}
	var buf [utf8.UTFMax * dupGram]byte
	emit := func(win []rune) {
		n := 0
		for _, r := range win {
			n += utf8.EncodeRune(buf[n:], r)
		}
		h := fnv32a(buf[:n])
		for i := 0; i < dupSigRows; i++ {
			if v := dupRowMix(h, i); v < sig[i] {
				sig[i] = v
			}
		}
	}
	if len(rs) <= dupGram {
		emit(rs)
		return sig
	}
	for i := 0; i+dupGram <= len(rs); i++ {
		emit(rs[i : i+dupGram])
	}
	return sig
}

// dupSim 两签名一致率（Jaccard 的 MinHash 无偏估计）。
func dupSim(a, b *[dupSigRows]uint32) float64 {
	m := 0
	for i := 0; i < dupSigRows; i++ {
		if a[i] == b[i] {
			m++
		}
	}
	return float64(m) / dupSigRows
}

// dupBandKeys LSH 分带桶键：每带 dupLSHRows 行签名混成 uint64（带号入键防跨带误撞）。
func dupBandKeys(sig *[dupSigRows]uint32) [dupLSHBands]uint64 {
	var keys [dupLSHBands]uint64
	for b := 0; b < dupLSHBands; b++ {
		h := uint64(14695981039346656037) ^ uint64(b)<<56
		for r := 0; r < dupLSHRows; r++ {
			h ^= uint64(sig[b*dupLSHRows+r])
			h *= 1099511628211
		}
		keys[b] = h
	}
	return keys
}

// dupDoc 扫描期单题驻留数据（定长签名 + 展示元数据；题面只留 80 字预览）。
type dupDoc struct {
	ProblemInfo
	Preview string
	Sig     [dupSigRows]uint32
}

// dupCluster 签名集 → 疑似重复组：LSH 同桶共现出候选 → 阈值验票 → 并查集聚类。
// 单桶超 dupMaxBucket 时只保留桶内签名全等的精确重复对（大桶几乎必然是全库共用
// 样板文本，跨签名候选纯噪声）。返回组按组内题数降序 → 相似度降序 → 标题升序，
// 组内按题号升序（docs 须已按题号升序）。
func dupCluster(docs []dupDoc) []DuplicateProblemGroup {
	buckets := map[uint64][]int32{}
	for i := range docs {
		for _, k := range dupBandKeys(&docs[i].Sig) {
			buckets[k] = append(buckets[k], int32(i))
		}
	}

	parent := make([]int32, len(docs))
	minSim := make([]float64, len(docs))
	for i := range parent {
		parent[i] = int32(i)
		minSim[i] = 1
	}
	var find func(int32) int32
	find = func(x int32) int32 {
		for parent[x] != x {
			parent[x] = parent[parent[x]] // 路径压缩半折
			x = parent[x]
		}
		return x
	}
	confirm := func(a, b int32, sim float64) {
		ra, rb := find(a), find(b)
		if ra == rb {
			if sim < minSim[ra] {
				minSim[ra] = sim
			}
			return
		}
		parent[rb] = ra
		if m := minSim[rb]; m < minSim[ra] {
			minSim[ra] = m
		}
		if sim < minSim[ra] {
			minSim[ra] = sim
		}
	}
	tryPair := func(a, b int32) {
		if sim := dupSim(&docs[a].Sig, &docs[b].Sig); sim >= dupSimThreshold {
			confirm(a, b, sim)
		}
	}

	for _, members := range buckets {
		if len(members) < 2 {
			continue
		}
		if len(members) > dupMaxBucket {
			bySig := map[[dupSigRows]uint32][]int32{}
			for _, i := range members {
				bySig[docs[i].Sig] = append(bySig[docs[i].Sig], i)
			}
			for _, g := range bySig { // 签名全等 = 精确重复，相似度恒 1.0
				for j := 1; j < len(g); j++ {
					confirm(g[0], g[j], 1)
				}
			}
			continue
		}
		for x := 0; x < len(members); x++ {
			for y := x + 1; y < len(members); y++ {
				tryPair(members[x], members[y])
			}
		}
	}

	grouped := map[int32][]int32{}
	for i := range docs {
		root := find(int32(i))
		grouped[root] = append(grouped[root], int32(i))
	}
	out := make([]DuplicateProblemGroup, 0, 4)
	for _, members := range grouped {
		if len(members) < 2 {
			continue
		}
		first := docs[members[0]] // docs 题号升序 → 首条即最小组号
		ps := make([]ProblemInfo, 0, len(members))
		for _, i := range members {
			ps = append(ps, docs[i].ProblemInfo)
		}
		out = append(out, DuplicateProblemGroup{
			Title: first.Title, Preview: first.Preview,
			Similarity: minSim[find(members[0])], Problems: ps,
		})
	}
	// 组内题数降序 → 相似度降序 → 标题稳定输出
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Problems) != len(out[j].Problems) {
			return len(out[i].Problems) > len(out[j].Problems)
		}
		if out[i].Similarity != out[j].Similarity {
			return out[i].Similarity > out[j].Similarity
		}
		return out[i].Title < out[j].Title
	})
	return out
}

// ScanDuplicateProblems 全库流式扫描，返回全部疑似重复组（组内 ≥2 题）。
func (r *Repo) ScanDuplicateProblems(ctx context.Context) ([]DuplicateProblemGroup, error) {
	cursor, err := r.problems.Find(ctx, bson.M{},
		options.Find().SetProjection(bson.M{"title": 1, "statement": 1, "type": 1, "hidden": 1, "duplicate": 1, "createdBy": 1, "createdAt": 1}).
			SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var row struct {
		ID        int64     `bson:"_id"`
		Title     string    `bson:"title"`
		Statement string    `bson:"statement"`
		Type      string    `bson:"type"`
		Hidden    bool      `bson:"hidden"`
		Duplicate bool      `bson:"duplicate"`
		CreatedBy int64     `bson:"createdBy"`
		CreatedAt time.Time `bson:"createdAt"`
	}
	docs := make([]dupDoc, 0, 64)
	for cursor.Next(ctx) {
		if err := cursor.Decode(&row); err != nil {
			return nil, err
		}
		// 标题并入文本：标题天然参与相似度（改标题的小动作撼不动题面主体）
		norm := normalizeDupText(row.Title + " " + row.Statement)
		docs = append(docs, dupDoc{
			ProblemInfo: ProblemInfo{
				ID: row.ID, Title: row.Title, Type: row.Type, Hidden: row.Hidden, Duplicate: row.Duplicate,
				CreatedBy: row.CreatedBy, CreatedAt: row.CreatedAt.Format(time.RFC3339),
			},
			Preview: truncateRunes(row.Statement, 80),
			Sig:     dupSigFromRunes([]rune(norm)),
		})
	}
	if err := cursor.Err(); err != nil {
		return nil, err
	}
	return dupCluster(docs), nil
}
