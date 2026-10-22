package repo

// migrateDataLayoutV2 一次性迁移（v0.15 统一题包格式，2026-10-22 拍板不留老账）：
// 测试数据卷 cases/<N>/in|ans 序号布局 → data/<原始文件名>。原导入时文件名被改成
// in/ans，原名已不可考，合成 test<N>.in / test<N>.out；checker/driver/template/
// interactor 固定名文件与 extra/ 存档一并挪进 data/；卷内 problem.yaml / config.yaml /
// statement.md 副本删除（权威导出取代卷内归档）；CaseMeta 更新文件名并废弃 sample 标记。

import (
	"context"
	"fmt"
	"log"
	"path"
	"sort"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"

	"soj/internal/testdata"
)

func (r *Repo) migrateDataLayoutV2(ctx context.Context, store testdata.Provider) error {
	cursor, err := r.Problems.Find(ctx, bson.M{})
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	var list []*Problem
	if err := cursor.All(ctx, &list); err != nil {
		return err
	}
	for _, p := range list {
		if p.IsObjective() {
			continue // 客观题无数据卷内容
		}
		if len(p.Cases) == 0 {
			continue
		}
		set := bson.M{}
		cases := make([]CaseMeta, 0, len(p.Cases))
		sorted := append([]CaseMeta(nil), p.Cases...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].Index < sorted[j].Index })
		for i, tc := range sorted {
			n := i + 1
			inName := fmt.Sprintf("data/test%d.in", n)
			ansName := ""
			if tc.AnsName != "" {
				ansName = fmt.Sprintf("data/test%d.out", n)
			}
			if err := moveVolumeFile(store, p.ID, tc.InName, inName); err != nil {
				log.Printf("[migrate][dataLayoutV2] 题目 %d 迁移 %s 失败: %v", p.ID, tc.InName, err)
				continue
			}
			if ansName != "" {
				if err := moveVolumeFile(store, p.ID, tc.AnsName, ansName); err != nil {
					return fmt.Errorf("题目 %d 迁移 %s: %w", p.ID, tc.AnsName, err)
				}
			}
			_ = store.RemoveDir(p.ID, fmt.Sprintf("cases/%d", tc.Index))
			cases = append(cases, CaseMeta{
				Index: n, InName: inName, AnsName: ansName,
				InSize: tc.InSize, AnsSize: tc.AnsSize, InSHA: tc.InSHA, AnsSHA: tc.AnsSHA,
			})
		}
		if len(cases) == 0 {
			continue
		}
		set["cases"] = cases
		set["sample"] = nil // 旧 sample 字段（CaseMeta 已删）
		unset := bson.M{}
		for field, old := range map[string]string{
			"checkerFile":    p.CheckerFile,
			"driverFile":     p.DriverFile,
			"templateFile":   p.TemplateFile,
			"interactorFile": p.InteractorFile,
		} {
			if old == "" || strings.HasPrefix(old, "data/") {
				continue
			}
			newRel := "data/" + path.Base(old)
			if err := moveVolumeFile(store, p.ID, old, newRel); err == nil {
				set[field] = newRel
			}
		}
		// extra/ 存档上翻为 data/<basename>（重名跳过——保数据不覆盖）
		_ = store.Walk(p.ID, func(rel string) error {
			if strings.HasPrefix(rel, "extra/") {
				newRel := "data/" + path.Base(rel)
				if _, err := store.Open(p.ID, newRel); err != nil {
					_ = moveVolumeFile(store, p.ID, rel, newRel)
				} else {
					_ = store.Delete(p.ID, rel)
				}
			}
			return nil
		})
		_ = store.RemoveDir(p.ID, "extra")
		_ = store.RemoveDir(p.ID, "cases")
		_ = store.Delete(p.ID, "problem.yaml")
		_ = store.Delete(p.ID, "config.yaml")
		_ = store.Delete(p.ID, "statement.md")
		update := bson.M{"$set": set}
		if len(unset) > 0 {
			update["$unset"] = unset
		}
		if _, err := r.Problems.UpdateOne(ctx, bson.M{"_id": p.ID}, update); err != nil {
			return err
		}
	}
	return nil
}

// moveVolumeFile 卷内文件移动（读-写-删；源不存在视为成功——幂等重跑安全）。
func moveVolumeFile(store testdata.Provider, pid int64, oldRel, newRel string) error {
	if oldRel == "" || oldRel == newRel {
		return nil
	}
	data, err := testdata.ReadAll(store, pid, oldRel)
	if err != nil {
		return nil // 源不存在（可能已迁移）——幂等
	}
	if err := store.WriteFile(pid, newRel, data); err != nil {
		return err
	}
	return store.Delete(pid, oldRel)
}
