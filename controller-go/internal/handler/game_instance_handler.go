package handler

import (
	"errors"
	"net/http"

	"controller-go/internal/biz"
	"controller-go/internal/entity"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GameInstanceHandler 提供 GameInstance 相关的 HTTP 接口
type GameInstanceHandler struct {
	gameInstanceUseCase *biz.GameInstanceUseCase
	// B-04/P1-1：实例运行时统计缓存（node_agent 探针心跳数据），nil 时 runtime 端点返回 unknown
	runtimeStats *biz.RuntimeStatsRegistry
}

func NewGameInstanceHandler(uc *biz.GameInstanceUseCase) *GameInstanceHandler {
	return &GameInstanceHandler{gameInstanceUseCase: uc}
}

// SetRuntimeStatsRegistry 附加实例运行时统计缓存（B-04/P1-1）
func (h *GameInstanceHandler) SetRuntimeStatsRegistry(reg *biz.RuntimeStatsRegistry) {
	h.runtimeStats = reg
}

// RegisterRoutes 注册 GameInstance 相关的路由
func (h *GameInstanceHandler) RegisterRoutes(router *gin.Engine) {
	group := router.Group("/api/game-instances")
	group.POST("", h.CreateGameInstance)
	group.GET("", h.ListGameInstances)
	group.GET("/:id", h.GetGameInstance)
	group.PUT("/:id/config", h.UpdateInstanceConfig)
	group.GET("/:id/ports", h.GetInstancePorts)
	group.GET("/:id/connect", h.GetInstanceConnect)
	group.GET("/:id/runtime", h.GetInstanceRuntime) // B-04/P1-1：健康 + 在线人数
	group.POST("/:id/start", h.StartGameInstance)
	group.POST("/:id/stop", h.StopGameInstance)
	group.POST("/:id/cancel", h.CancelGameInstance)
	group.POST("/:id/retry", h.RetryGameInstance)
	group.POST("/:id/dispatch", h.ForceDispatch)
	group.DELETE("/:id", h.DeleteGameInstance)
}

type createGameInstanceRequest struct {
	GameID      string                  `json:"game_id"`
	GameBuildID string                  `json:"game_build_id"`
	Region      string                  `json:"region,omitempty"`               // R3 区域偏好
	Priority    int                     `json:"priority,omitempty"`             // D7 优先级（默认 100）
	Resources   *entity.ResourceRequest `json:"resources,omitempty"`            // 显式资源覆盖（创建时指定生效）
	Config      map[string]string       `json:"config,omitempty"`               // 000024：实例配置（platform+player 合并，adapter.toml schema 校验）
	// 000027（M10）：订阅归属。nil = 未归属（老实例豁免单活跃约束）。
	// 创建时仅记录归属（初始 stopped 不占槽位），单活跃约束在 start/retry 校验。
	SubscriptionID *string `json:"subscription_id,omitempty"`
}

// CreateGameInstance 新建 game_instance，初始状态为 StatusStopped。
// game_build_id 可选；未传时以 "public" channel 解析最新可用构建。
func (h *GameInstanceHandler) CreateGameInstance(c *gin.Context) {
	var req createGameInstanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}
	if req.GameID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "game_id is required"})
		return
	}

	instance, err := h.gameInstanceUseCase.CreateGameInstance(c.Request.Context(), req.GameID, biz.CreateInstanceOptions{
		GameBuildID:    req.GameBuildID,
		Region:         req.Region,
		Priority:       req.Priority,
		Resources:      req.Resources,
		Config:         req.Config,
		SubscriptionID: req.SubscriptionID,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, instance)
}

// GetGameInstance 获取 game_instance 当前状态
func (h *GameInstanceHandler) GetGameInstance(c *gin.Context) {
	id := c.Param("id")
	instance, err := h.gameInstanceUseCase.GetGameInstance(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "instance not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, instance)
}

type updateInstanceConfigRequest struct {
	Config map[string]string `json:"config"`
}

// UpdateInstanceConfig 更新实例配置（schema 校验后落库，重启生效）
func (h *GameInstanceHandler) UpdateInstanceConfig(c *gin.Context) {
	var req updateInstanceConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}
	instance, err := h.gameInstanceUseCase.UpdateInstanceConfig(c.Request.Context(), c.Param("id"), req.Config)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, instance)
}

// StartGameInstance 启动 game_instance，状态置为 StatusPending 进入调度
func (h *GameInstanceHandler) StartGameInstance(c *gin.Context) {
	id := c.Param("id")
	if err := h.gameInstanceUseCase.StartGameInstance(c.Request.Context(), id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "instance not found"})
			return
		}
		if errors.Is(err, biz.ErrSubscriptionConflict) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, biz.ErrStopFailure) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "started"})
}

// StopGameInstance 停止 game_instance，状态置为 StatusStopping 进入调度
func (h *GameInstanceHandler) StopGameInstance(c *gin.Context) {
	id := c.Param("id")
	if err := h.gameInstanceUseCase.StopGameInstance(c.Request.Context(), id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "instance not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "stopping"})
}

// ListGameInstances 列出实例，支持 ?status=<状态字符串> 与 ?subscription_id=<订阅ID>（M11）过滤
func (h *GameInstanceHandler) ListGameInstances(c *gin.Context) {
	var status *entity.InstanceStatus
	if s := c.Query("status"); s != "" {
		parsed, ok := entity.ParseInstanceStatus(s)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid status: " + s})
			return
		}
		status = &parsed
	}

	instances, err := h.gameInstanceUseCase.ListGameInstances(c.Request.Context(), status, c.Query("subscription_id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"instances": instances})
}

// GetInstancePorts 查询实例已分配的端口映射（调试用）
func (h *GameInstanceHandler) GetInstancePorts(c *gin.Context) {
	id := c.Param("id")
	ports, err := h.gameInstanceUseCase.GetInstancePorts(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"instance_id": id, "ports": ports})
}

// GetInstanceConnect 查询实例对客户端公开的连接地址（node_ip:game_host_port）
func (h *GameInstanceHandler) GetInstanceConnect(c *gin.Context) {
	id := c.Param("id")
	info, err := h.gameInstanceUseCase.GetInstanceConnectInfo(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "instance not found"})
			return
		}
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, info)
}

// GetInstanceRuntime 查询实例运行时统计（B-04/P1-1：在线人数 + 健康）。
// 数据来自 node_agent 探针心跳缓存；三态：running 且有数据 / running 尚无数据（unknown）/
// 非 running（不采集）。
func (h *GameInstanceHandler) GetInstanceRuntime(c *gin.Context) {
	id := c.Param("id")
	inst, err := h.gameInstanceUseCase.GetGameInstance(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "instance not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if inst.Status != entity.StatusRunning {
		c.JSON(http.StatusOK, gin.H{"instance_id": id, "running": false})
		return
	}
	if h.runtimeStats == nil {
		c.JSON(http.StatusOK, gin.H{"instance_id": id, "running": true, "probe_mode": "unknown", "healthy": false})
		return
	}
	stat, ok := h.runtimeStats.Get(id)
	if !ok {
		c.JSON(http.StatusOK, gin.H{"instance_id": id, "running": true, "probe_mode": "unknown", "healthy": false})
		return
	}
	c.JSON(http.StatusOK, stat)
}

// RetryGameInstance 重试失败实例：failed → pending 重新入队调度
func (h *GameInstanceHandler) RetryGameInstance(c *gin.Context) {
	id := c.Param("id")
	if err := h.gameInstanceUseCase.RetryGameInstance(c.Request.Context(), id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "instance not found"})
			return
		}
		if errors.Is(err, biz.ErrSubscriptionConflict) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "retrying"})
}

// CancelGameInstance 取消排队（D5）：移除出队，实例保持 stopped。仅 queued 状态允许。
func (h *GameInstanceHandler) CancelGameInstance(c *gin.Context) {
	id := c.Param("id")
	if err := h.gameInstanceUseCase.CancelGameInstance(c.Request.Context(), id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "instance not found"})
			return
		}
		if errors.Is(err, biz.ErrNotQueued) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "cancelled"})
}

// ForceDispatch 跳过状态校验，强制把实例压入调度队列（调试用）
func (h *GameInstanceHandler) ForceDispatch(c *gin.Context) {
	id := c.Param("id")
	if err := h.gameInstanceUseCase.ForceDispatch(c.Request.Context(), id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "instance not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "dispatched"})
}

// DeleteGameInstance 删除实例（非运行/非调度中状态；同时清理端口映射）
func (h *GameInstanceHandler) DeleteGameInstance(c *gin.Context) {
	id := c.Param("id")
	if err := h.gameInstanceUseCase.DeleteGameInstance(c.Request.Context(), id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "instance not found"})
			return
		}
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}