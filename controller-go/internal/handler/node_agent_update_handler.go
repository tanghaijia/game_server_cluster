package handler

import (
	"errors"
	"net/http"

	"controller-go/internal/biz"

	"github.com/gin-gonic/gin"
)

// NodeAgentUpdateHandler node_agent 一键更新（P3，见 docs/node-agent-upgrade-design.md §3.3）
type NodeAgentUpdateHandler struct {
	orchestrator *biz.NodeAgentUpdateOrchestrator
	releaseUC    *biz.AgentReleaseUseCase
}

func NewNodeAgentUpdateHandler(orchestrator *biz.NodeAgentUpdateOrchestrator, releaseUC *biz.AgentReleaseUseCase) *NodeAgentUpdateHandler {
	return &NodeAgentUpdateHandler{orchestrator: orchestrator, releaseUC: releaseUC}
}

// RegisterRoutes 注册更新路由
func (h *NodeAgentUpdateHandler) RegisterRoutes(router *gin.Engine) {
	group := router.Group("/api/node-agents")
	group.POST("/batch-update", h.BatchUpdate)
	group.POST("/:id/rollback", h.Rollback)
}

type batchUpdateRequest struct {
	// 每项：agent_id + release_id（目标发布版本）
	Updates []struct {
		AgentID   string `json:"agent_id"`
		ReleaseID string `json:"release_id"`
	} `json:"updates"`
}

// BatchUpdate 批量滚动更新（串行；每节点独立结果，互不阻塞）
func (h *NodeAgentUpdateHandler) BatchUpdate(c *gin.Context) {
	var req batchUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}
	if len(req.Updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "updates 不能为空"})
		return
	}
	targets := make([]biz.NodeAgentUpdateTarget, 0, len(req.Updates))
	for _, u := range req.Updates {
		if u.AgentID == "" || u.ReleaseID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "agent_id 与 release_id 必填"})
			return
		}
		targets = append(targets, biz.NodeAgentUpdateTarget{AgentID: u.AgentID, ReleaseID: u.ReleaseID})
	}
	results := h.orchestrator.Update(c.Request.Context(), targets)
	c.JSON(http.StatusOK, gin.H{"results": results})
}

// Rollback 回滚指定 node_agent 到目标 release（走同一更新链路；admin 前端从清单选目标版本）
func (h *NodeAgentUpdateHandler) Rollback(c *gin.Context) {
	var req struct {
		ReleaseID string `json:"release_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}
	if req.ReleaseID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "release_id 必填"})
		return
	}
	// release 必须存在
	if _, err := h.releaseUC.Get(c.Request.Context(), req.ReleaseID); err != nil {
		if errors.Is(err, biz.ErrReleaseNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "release not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	results := h.orchestrator.Update(c.Request.Context(), []biz.NodeAgentUpdateTarget{{
		AgentID: c.Param("id"), ReleaseID: req.ReleaseID,
	}})
	if len(results) == 0 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no result"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"result": results[0]})
}
