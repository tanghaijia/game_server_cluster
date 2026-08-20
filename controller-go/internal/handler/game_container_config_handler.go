package handler

import (
	"errors"
	"net/http"

	"controller-go/internal/biz"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GameContainerConfigHandler 游戏容器配置（图形化配置 API）：
// GET/PUT /api/games/:id/container-config —— 端口模式/片段、注入模式、资源默认值、single_threaded。
type GameContainerConfigHandler struct {
	useCase *biz.GameContainerConfigUseCase
}

func NewGameContainerConfigHandler(uc *biz.GameContainerConfigUseCase) *GameContainerConfigHandler {
	return &GameContainerConfigHandler{useCase: uc}
}

func (h *GameContainerConfigHandler) RegisterRoutes(router *gin.Engine) {
	group := router.Group("/api/games")
	group.GET("/:id/container-config", h.GetConfig)
	group.PUT("/:id/container-config", h.UpdateConfig)
}

// GetConfig 返回游戏容器配置（含端口片段）
func (h *GameContainerConfigHandler) GetConfig(c *gin.Context) {
	config, err := h.useCase.GetConfig(c.Request.Context(), c.Param("id"))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "game or container config not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, config)
}

// UpdateConfig 更新容器配置（端口片段整体替换）
func (h *GameContainerConfigHandler) UpdateConfig(c *gin.Context) {
	var req biz.ContainerConfigUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}
	config, err := h.useCase.UpdateConfig(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "game or container config not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, config)
}
