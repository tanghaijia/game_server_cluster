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
}

func NewGameInstanceHandler(uc *biz.GameInstanceUseCase) *GameInstanceHandler {
	return &GameInstanceHandler{gameInstanceUseCase: uc}
}

// RegisterRoutes 注册 GameInstance 相关的路由
func (h *GameInstanceHandler) RegisterRoutes(router *gin.Engine) {
	group := router.Group("/api/game-instances")
	group.POST("", h.CreateGameInstance)
	group.GET("", h.ListGameInstances)
	group.GET("/:id", h.GetGameInstance)
	group.GET("/:id/ports", h.GetInstancePorts)
	group.GET("/:id/connect", h.GetInstanceConnect)
	group.POST("/:id/start", h.StartGameInstance)
	group.POST("/:id/stop", h.StopGameInstance)
	group.POST("/:id/cancel", h.CancelGameInstance)
	group.POST("/:id/retry", h.RetryGameInstance)
	group.POST("/:id/dispatch", h.ForceDispatch)
	group.DELETE("/:id", h.DeleteGameInstance)
}

type createGameInstanceRequest struct {
	GameID      string                `json:"game_id"`
	GameBuildID string                `json:"game_build_id"`
	Region      string                `json:"region,omitempty"`               // R3 区域偏好
	Priority    int                   `json:"priority,omitempty"`             // D7 优先级（默认 100）
	Resources   *entity.ResourceRequest `json:"resources,omitempty"`          // 显式资源覆盖（创建时指定生效）
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
		GameBuildID: req.GameBuildID,
		Region:      req.Region,
		Priority:    req.Priority,
		Resources:   req.Resources,
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

// StartGameInstance 启动 game_instance，状态置为 StatusPending 进入调度
func (h *GameInstanceHandler) StartGameInstance(c *gin.Context) {
	id := c.Param("id")
	if err := h.gameInstanceUseCase.StartGameInstance(c.Request.Context(), id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "instance not found"})
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

// ListGameInstances 列出全部实例，支持 ?status=<状态字符串> 过滤
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

	instances, err := h.gameInstanceUseCase.ListGameInstances(c.Request.Context(), status)
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

// RetryGameInstance 重试失败实例：failed → pending 重新入队调度
func (h *GameInstanceHandler) RetryGameInstance(c *gin.Context) {
	id := c.Param("id")
	if err := h.gameInstanceUseCase.RetryGameInstance(c.Request.Context(), id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "instance not found"})
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