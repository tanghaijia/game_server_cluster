package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"controller-go/internal/biz"

	"github.com/gin-gonic/gin"
)

// ObserverHandler 调度观测接口（管理员视角）：
// /api/observe/*（节点/历史/队列/事件/统计/试调度）+ /metrics（Prometheus 文本）。
type ObserverHandler struct {
	observerUseCase *biz.ObserverUseCase
}

func NewObserverHandler(uc *biz.ObserverUseCase) *ObserverHandler {
	return &ObserverHandler{observerUseCase: uc}
}

func (h *ObserverHandler) RegisterRoutes(router *gin.Engine) {
	g := router.Group("/api/observe")
	g.GET("/nodes", h.NodesOverview)
	g.GET("/nodes/:id/history", h.NodeHistory)
	g.GET("/cache", h.CacheOverview)
	g.GET("/queue", h.QueueOverview)
	g.GET("/events", h.Events)
	g.GET("/scheduler/stats", h.SchedulerStats)
	g.POST("/scheduler/preview", h.PreviewSchedule)
	router.GET("/metrics", h.Metrics)
}

// NodesOverview 节点资源总览
func (h *ObserverHandler) NodesOverview(c *gin.Context) {
	nodes, err := h.observerUseCase.NodesOverview(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"nodes": nodes})
}

// NodeHistory 节点资源采样历史（?window=1h）
func (h *ObserverHandler) NodeHistory(c *gin.Context) {
	window := time.Hour
	if w := c.Query("window"); w != "" {
		if d, err := time.ParseDuration(w); err == nil {
			window = d
		}
	}
	samples, err := h.observerUseCase.NodeHistory(c.Request.Context(), c.Param("id"), window)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"node_id": c.Param("id"), "samples": samples})
}

// CacheOverview 全部 enabled 节点的 game-cache 状态（管理员观测）
func (h *ObserverHandler) CacheOverview(c *gin.Context) {
	cache, err := h.observerUseCase.CacheOverview(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"cache": cache})
}

// QueueOverview 排队详情
func (h *ObserverHandler) QueueOverview(c *gin.Context) {
	qs, err := h.observerUseCase.QueueOverview(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"queue": qs, "len": len(qs)})
}

// Events 调度事件流（?type=&limit=）
func (h *ObserverHandler) Events(c *gin.Context) {
	limit := 100
	if l := c.Query("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	typ := biz.SchedulerEventType(c.Query("type"))
	events := h.observerUseCase.Events(limit, typ)
	c.JSON(http.StatusOK, gin.H{"events": events})
}

// SchedulerStats 调度统计
func (h *ObserverHandler) SchedulerStats(c *gin.Context) {
	stats := h.observerUseCase.SchedulerStats(c.Request.Context())
	c.JSON(http.StatusOK, stats)
}

// PreviewSchedule 试调度干跑（不预留、不落库）
func (h *ObserverHandler) PreviewSchedule(c *gin.Context) {
	var req biz.PreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}
	if req.GameID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "game_id is required"})
		return
	}
	res, err := h.observerUseCase.PreviewSchedule(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}

// Metrics Prometheus 文本格式指标（S29；手写无依赖）
func (h *ObserverHandler) Metrics(c *gin.Context) {
	stats := h.observerUseCase.SchedulerStats(c.Request.Context())
	var b strings.Builder
	b.WriteString("# HELP schedule_attempts_total 调度尝试次数（按结果分）\n# TYPE schedule_attempts_total counter\n")
	for _, k := range []string{"scheduled", "queued", "failed"} {
		b.WriteString(fmt.Sprintf("schedule_attempts_total{result=%q} %d\n", k, stats.Attempts[k]))
	}
	b.WriteString("# HELP scheduler_queue_depth 排队实例数\n# TYPE scheduler_queue_depth gauge\n")
	b.WriteString(fmt.Sprintf("scheduler_queue_depth %d\n", stats.QueueLen))
	b.WriteString("# HELP scheduler_event_total 调度事件总数（内存缓冲）\n# TYPE scheduler_event_total gauge\n")
	b.WriteString(fmt.Sprintf("scheduler_event_total %d\n", stats.EventCount))
	c.Data(http.StatusOK, "text/plain; version=0.0.4; charset=utf-8", []byte(b.String()))
}
