package handler

import (
	"net/http"
	"net/http/pprof"
	"runtime"
	"runtime/debug"

	"controller-go/internal/biz"

	"github.com/gin-gonic/gin"
)

// DebugHandler 提供健康检查与调试接口（/healthz、/debug/*）
type DebugHandler struct {
	debugUseCase *biz.DebugUseCase
}

func NewDebugHandler(uc *biz.DebugUseCase) *DebugHandler {
	return &DebugHandler{debugUseCase: uc}
}

// RegisterRoutes 注册健康检查与调试路由
func (h *DebugHandler) RegisterRoutes(router *gin.Engine) {
	router.GET("/healthz", h.Health)

	router.GET("/debug/version", h.Version)
	router.GET("/debug/reconcile", h.ReconcileStatus)
	router.POST("/debug/reconcile/recover", h.Recover)
	router.GET("/debug/instances", h.InstanceOverview)

	// Go pprof（goroutine / heap / profile / trace 等）
	pprofMux := http.NewServeMux()
	pprofMux.HandleFunc("/debug/pprof/", pprof.Index)
	pprofMux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	pprofMux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	pprofMux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	pprofMux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	router.GET("/debug/pprof/*any", gin.WrapH(pprofMux))
}

// Health 存活探针
func (h *DebugHandler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Version 返回构建信息（go.mod 模块版本 + Go 版本）
func (h *DebugHandler) Version(c *gin.Context) {
	info := gin.H{
		"go_version":     runtime.Version(),
		"module_path":    "",
		"module_version": "",
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Path != "" {
		info["module_path"] = bi.Main.Path
		info["module_version"] = bi.Main.Version
	}
	c.JSON(http.StatusOK, info)
}

// ReconcileStatus 调度器运行状态：队列长度、重试计数、中间态实例、调度器状态
func (h *DebugHandler) ReconcileStatus(c *gin.Context) {
	status, err := h.debugUseCase.ReconcileStatus(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, status)
}

// Recover 手动触发调度恢复（中间态实例重新入队）
func (h *DebugHandler) Recover(c *gin.Context) {
	queueLen, err := h.debugUseCase.Recover(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "recovered", "queue_len": queueLen})
}

// InstanceOverview 全部实例聚合视图：实例 + 端口映射 + node_agent 地址
func (h *DebugHandler) InstanceOverview(c *gin.Context) {
	overview, err := h.debugUseCase.InstanceOverview(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"instances": overview})
}
