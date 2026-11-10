package repo

// 整理历史（v0.92 题库整理二期）：apply（AI 预览确认 / 手动整理两路）批次留痕——
// 谁、何时、逐题 old→new 可溯可审计；全量留存（拍板口径：低频管理操作，量级无压力）。
// 标签明细存名字快照而非 ID——树后续改名/删节点不影响历史回看。

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// TidyHistoryItem 一题的变更明细（kind 专属字段按批次类型填充；失败项 old==库内现值、
// new==尝试写入的最终值 + err 原因）。
type TidyHistoryItem struct {
	PID   int64  `bson:"pid" json:"pid"`
	Title string `bson:"title" json:"title"`
	OK    bool   `bson:"ok" json:"ok"`
	Err   string `bson:"err,omitempty" json:"err,omitempty"`

	OldDifficulty int `bson:"oldDifficulty,omitempty" json:"old_difficulty,omitempty"` // 0 = 未设置
	NewDifficulty int `bson:"newDifficulty,omitempty" json:"new_difficulty,omitempty"` // 失败项 = 尝试值

	OldTagNames []string `bson:"oldTagNames,omitempty" json:"old_tag_names,omitempty"`
	NewTagNames []string `bson:"newTagNames,omitempty" json:"new_tag_names,omitempty"`

	OldMetaNames []string `bson:"oldMetaNames,omitempty" json:"old_meta_names,omitempty"`
	NewMetaNames []string `bson:"newMetaNames,omitempty" json:"new_meta_names,omitempty"`
}

// TidyHistory 一次 apply 批次（mode=ai：AI 建议确认应用；mode=manual：手动整理保存）。
type TidyHistory struct {
	ID      int64             `bson:"_id" json:"id"`
	At      time.Time         `bson:"at" json:"at"`
	ByUID   int64             `bson:"byUid" json:"by_uid"`
	ByName  string            `bson:"byName" json:"by_name"`
	Mode    string            `bson:"mode" json:"mode"`   // ai | manual
	Kinds   []string          `bson:"kinds" json:"kinds"` // 触及的整理类（ai 单类；manual 可混合）
	From    int64             `bson:"from" json:"from"`   // 生成建议任务的题号范围（0 = 不限/手动）
	To      int64             `bson:"to" json:"to"`
	Applied int               `bson:"applied" json:"applied"`
	Failed  int               `bson:"failed" json:"failed"`
	Message string            `bson:"message,omitempty" json:"message,omitempty"`
	Items   []TidyHistoryItem `bson:"items" json:"items"`
}

// InsertTidyHistory 落一条批次历史（apply 链尾部，失败不致命只少一条审计）。
func (r *Repo) InsertTidyHistory(ctx context.Context, h *TidyHistory) error {
	id, err := r.nextSeq(ctx, "tidyHistory")
	if err != nil {
		return err
	}
	h.ID = id
	_, err = r.tidyHistory.InsertOne(ctx, h)
	return err
}

// ListTidyHistory 批次列表（倒序分页，列表不带 items 明细——明细按需点开单批拉取）。
func (r *Repo) ListTidyHistory(ctx context.Context, page, pageSize int) ([]*TidyHistory, int64, error) {
	total, err := r.tidyHistory.CountDocuments(ctx, bson.M{})
	if err != nil {
		return nil, 0, err
	}
	cursor, err := r.tidyHistory.Find(ctx, bson.M{},
		options.Find().
			SetProjection(bson.M{"items": 0}).
			SetSort(bson.D{{Key: "_id", Value: -1}}).
			SetSkip(int64((page - 1) * pageSize)).
			SetLimit(int64(pageSize)))
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)
	list := make([]*TidyHistory, 0, pageSize)
	if err := cursor.All(ctx, &list); err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// GetTidyHistory 单批全量（含逐题明细）；不存在返回 nil, nil（调用方 404）。
func (r *Repo) GetTidyHistory(ctx context.Context, id int64) (*TidyHistory, error) {
	var h TidyHistory
	if err := r.tidyHistory.FindOne(ctx, bson.M{"_id": id}).Decode(&h); err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return &h, nil
}
