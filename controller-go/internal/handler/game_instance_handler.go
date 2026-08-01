package handler

import (
	"errors"
	"net/http"

	"controller-go/internal/biz"

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
	group.GET("/:id", h.GetGameInstance)
	group.POST("/:id/start", h.StartGameInstance)
	group.POST("/:id/stop", h.StopGameInstance)
}

type createGameInstanceRequest struct {
	GameID string `json:"game_id"`
}

// CreateGameInstance 新建 game_instance，初始状态为 StatusStopped
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

	instance, err := h.gameInstanceUseCase.CreateGameInstance(c.Request.Context(), req.GameID)
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
