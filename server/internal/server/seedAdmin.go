package server

// 种子题管理（后台面板）：清除 / 重新部署种子目录（SOJ_SEED_DIR）下的题包。
// 种子集与启动播种同口径——数字前缀目录按题号、无前缀目录按标题对账（见
// importer.SeedInventory）；root 专属（部署级操作，同标签树/角色管理先例）。

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

// importer 数据卷感知的导入器构造（种子管理与资产异步转存共用）。
func (h *Handlers) importer() *repo.Importer {
	return &repo.Importer{Repo: h.repo, DataDir: h.cfg.DataDir, TransferImages: true}
}

// seedImporter 种子链的导入器（同 importer，语义别名保留给种子调用点）。
func (h *Handlers) seedImporter() *repo.Importer { return h.importer() }

// AdminSeedStatus GET /admin/seed：种子目录 ↔ 库内对账（面板展示与操作前预览）。
func (h *Handlers) AdminSeedStatus(c *gin.Context) {
	marked, err := h.repo.SeedMarked(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取种子状态失败")
		return
	}
	if h.cfg.SeedDir == "" {
		ok(c, gin.H{"seed_dir": "", "marked": marked, "rows": []repo.SeedRow{}})
		return
	}
	rows, err := h.seedImporter().SeedInventory(c.Request.Context(), h.cfg.SeedDir)
	if err != nil {
		log.Printf("[seed] 对账失败: %v", err)
		fail(c, http.StatusInternalServerError, "读取种子目录失败")
		return
	}
	ok(c, gin.H{"seed_dir": h.cfg.SeedDir, "marked": marked, "rows": rows})
}

// AdminSeedDeploy POST /admin/seed/deploy：重新部署种子题（补缺不覆盖；已存在的题
// 跳过保护线上修改，清除后再部署即全量回填）。逐目录结果随响应返回，前端汇总展示。
func (h *Handlers) AdminSeedDeploy(c *gin.Context) {
	if h.cfg.SeedDir == "" {
		fail(c, http.StatusBadRequest, "未配置种子目录（SOJ_SEED_DIR）")
		return
	}
	outcomes, err := h.seedImporter().SeedDeploy(c.Request.Context(), h.cfg.SeedDir)
	if err != nil {
		log.Printf("[seed] 部署失败: %v", err)
		fail(c, http.StatusInternalServerError, "部署种子题失败")
		return
	}
	ok(c, gin.H{"outcomes": outcomes})
}

// AdminSeedClear POST /admin/seed/clear：清除种子题（题目 + 数据卷 + 提交 + 挑战 +
// 题解 + 题目讨论一并删除，删除统计随响应返回）。
func (h *Handlers) AdminSeedClear(c *gin.Context) {
	if h.cfg.SeedDir == "" {
		fail(c, http.StatusBadRequest, "未配置种子目录（SOJ_SEED_DIR）")
		return
	}
	stats, err := h.seedImporter().SeedClear(c.Request.Context(), h.cfg.SeedDir)
	if err != nil {
		log.Printf("[seed] 清除失败: %v", err)
		fail(c, http.StatusInternalServerError, "清除种子题失败")
		return
	}
	ok(c, gin.H{"stats": stats})
}
