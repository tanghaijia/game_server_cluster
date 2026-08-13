package handler

import (
	"errors"
	"net/http"

	"controller-go/internal/biz"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GameCacheHandler 提供 Steam 分支与游戏缓存相关的 HTTP 接口（调试/运维用）
type GameCacheHandler struct {
	gameCacheManager *biz.GameCacheManager
}

func NewGameCacheHandler(m *biz.GameCacheManager) *GameCacheHandler {
	return &GameCacheHandler{gameCacheManager: m}
}

// RegisterRoutes 注册分支与缓存相关的路由
func (h *GameCacheHandler) RegisterRoutes(router *gin.Engine) {
	group := router.Group("/api/games")
	group.GET("/:id/branches", h.ListBranches)
	group.POST("/:id/branches/sync", h.SyncBranches)
	group.POST("/:id/branches/:branch/cache", h.UpdateBranchCache)

	agentGroup := router.Group("/api/node-agents")
	agentGroup.GET("/:id/cache", h.GetNodeCache)
}

// ListBranches 列出某 game 已同步的 Steam 分支
func (h *GameCacheHandler) ListBranches(c *gin.Context) {
	gameID := c.Param("id")
	branches, err := h.gameCacheManager.ListNodeBranches(c.Request.Context(), gameID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"game_id": gameID, "branches": branches})
}

// SyncBranches 手动触发分支同步（从 asset_service 拉取并记录到本地表）
func (h *GameCacheHandler) SyncBranches(c *gin.Context) {
	gameID := c.Param("id")
	if err := h.gameCacheManager.SyncAndRecordBranch(c.Request.Context(), gameID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "synced"})
}

type updateBranchCacheRequest struct {
	NodeAgentID string `json:"node_agent_id"`
}

// UpdateBranchCache 手动触发指定 node_agent 上某 (game, branch) 的缓存检查/下载/更新
func (h *GameCacheHandler) UpdateBranchCache(c *gin.Context) {
	var req updateBranchCacheRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}
	if req.NodeAgentID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "node_agent_id is required"})
		return
	}

	err := h.gameCacheManager.UpdateBranchCache(c.Request.Context(), c.Param("id"), c.Param("branch"), req.NodeAgentID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "branch not found for game, sync first"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "cache ok"})
}

// GetNodeCache 查询指定 node_agent 上某 (game, branch) 的缓存状态
// 查询参数：game_id（必填）、branch（必填，默认 public）
func (h *GameCacheHandler) GetNodeCache(c *gin.Context) {
	nodeAgentID := c.Param("id")
	gameID := c.Query("game_id")
	branch := c.Query("branch")
	if branch == "" {
		branch = "public"
	}

	cache, err := h.gameCacheManager.GetNodeCache(c.Request.Context(), nodeAgentID, gameID, branch)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "node_agent not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"node_agent_id": nodeAgentID, "game_id": gameID, "branch": branch, "cache": cache})
}
