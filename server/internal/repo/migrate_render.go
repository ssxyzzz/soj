package repo

// 存量 md 渲染缓存迁移（v0.30 写入时渲染）：Bootstrap 后台渐进补齐各集合的
// html 渲染缓存（缺失补 / lute 版本变更全量重刷），批处理避免压启动期 Mongo。
// md 永远是唯一事实源：迁移失败/未完成只导致前端回退 md 客户端渲染，不坏。

import (
	"context"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"soj/internal/mdrender"
)

// renderedMetaKey meta.migrations 风格的完成标记键（meta 集合 rendered 键下）。
const renderedMetaKey = "renderedWithLute"

// renderTargets 参与写入时渲染的集合与字段对（md 事实源 → html 缓存）。
func renderTargets(r *Repo) []struct {
	coll     *mongo.Collection
	md, html string
} {
	return []struct {
		coll     *mongo.Collection
		md, html string
	}{
		{r.Problems, "statement", "statementHtml"},
		{r.roundsCol(KindContest), "intro", "introHtml"},
		{r.roundsCol(KindExam), "intro", "introHtml"},
		{r.db.Collection("assignments"), "intro", "introHtml"},
		{r.db.Collection("classes"), "intro", "introHtml"},
		{r.Lists, "intro", "introHtml"},
		{r.Solutions, "content", "contentHtml"},
	}
}

// MigrateRenderedHtml 渐进补齐渲染缓存：html 缺失的文档逐批渲染补写。
// refetch 全量重刷模式（lute 版本变更时）：不筛缺失，全部重渲染。
// 每批 200 条、批间 50ms 让路启动流量；返回处理的文档总数。
func (r *Repo) MigrateRenderedHtml(ctx context.Context, refetch bool) (int, error) {
	total := 0
	for _, t := range renderTargets(r) {
		filter := bson.M{}
		if !refetch {
			// 只补缺失：html 字段不存在（历史文档）；空串 md 渲染结果也是空串，跳过无意义写入
			filter[t.md] = bson.M{"$gt": ""}
			filter[t.html] = bson.M{"$exists": false}
		}
		for {
			// 通用解码：字段名动态，用 bson.M 原始读
			cursor, err := t.coll.Find(ctx, filter,
				options.Find().SetProjection(bson.M{"_id": 1, t.md: 1}).
					SetSort(bson.D{{Key: "_id", Value: 1}}).
					SetLimit(200))
			if err != nil {
				return total, err
			}
			rows := make([]bson.M, 0)
			if err := cursor.All(ctx, &rows); err != nil {
				return total, err
			}
			if len(rows) == 0 {
				break
			}
			wrote := 0
			for _, row := range rows {
				md, _ := row[t.md].(string)
				id := row["_id"]
				// 补齐模式带 $exists:false 守卫：批内 Find 与 UpdateOne 之间若用户
				// 编辑了 md，正常写入路径已写好新 html——无条件 $set 会用旧 md 的
				// 渲染结果覆盖之，且增量迁移按「html 已存在」跳过导致旧渲染永久滞留
				//（后端快赢批根修）。refetch 重刷模式本意就是覆盖，不加守卫。
				uf := bson.M{"_id": id}
				if !refetch {
					uf[t.html] = bson.M{"$exists": false}
				}
				if _, err := t.coll.UpdateOne(ctx,
					uf,
					bson.M{"$set": bson.M{t.html: mdrender.Render(md)}}); err == nil {
					wrote++
				}
			}
			total += wrote
			if wrote == 0 || len(rows) < 200 {
				break
			}
			// 已处理批次剔除：filter 加 _id 游标（refetch 模式下 filter 无条件，靠游标推进）
			lastID, _ := rows[len(rows)-1]["_id"].(int64)
			filter["_id"] = bson.M{"$gt": lastID}
			select {
			case <-ctx.Done():
				return total, ctx.Err()
			case <-time.After(50 * time.Millisecond):
			}
		}
	}
	return total, nil
}

// bootstrapRenderedHtmlAsync 启动后台迁移：首次（无标记）或 lute 版本变更时全量，
// 其余增量补缺失。完成后写 meta 标记；失败仅记日志（下次启动重试）。
func (r *Repo) bootstrapRenderedHtmlAsync() {
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[render-migrate] 迁移任务 panic: %v", rec)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		var meta struct {
			Rendered map[string]string `bson:"rendered"`
		}
		_ = r.db.Collection("meta").FindOne(ctx, bson.M{"_id": "rendered"}).Decode(&meta)
		prev, hasPrev := meta.Rendered[renderedMetaKey]

		refetch := hasPrev && prev != mdrender.LuteVersion // 版本变更：全量重刷
		n, err := r.MigrateRenderedHtml(ctx, refetch)
		if err != nil {
			log.Printf("[render-migrate] 渲染缓存迁移失败（下次启动重试）: %v", err)
			return
		}
		if _, err := r.db.Collection("meta").UpdateOne(ctx,
			bson.M{"_id": "rendered"},
			bson.M{"$set": bson.M{"rendered." + renderedMetaKey: mdrender.LuteVersion}},
			options.UpdateOne().SetUpsert(true)); err != nil {
			log.Printf("[render-migrate] 版本标记写入失败: %v", err)
			return
		}
		if n > 0 || refetch {
			log.Printf("[render-migrate] 渲染缓存迁移完成：%d 份文档（lute %s，%s）",
				n, mdrender.LuteVersion, map[bool]string{true: "全量重刷", false: "补缺失"}[refetch])
		}
	}()
}
