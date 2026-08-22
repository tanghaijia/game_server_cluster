package handler

import (
	"net/http"

	"controller-go/internal/biz"

	"github.com/gin-gonic/gin"
)

// GamePlatformConfigHandler 平台运营方配置（control=platform 项，按游戏全局）。
// 供 admin 管理平台配置（platform-service 透传 /api/admin/games/:id/platform-config）。
type GamePlatformConfigHandler struct {
	useCase *biz.PlatformConfigUseCase
}

func NewGamePlatformConfigHandler(uc *biz.PlatformConfigUseCase) *GamePlatformConfigHandler {
	return &GamePlatformConfigHandler{useCase: uc}
}

func (h *GamePlatformConfigHandler) RegisterRoutes(router *gin.Engine) {
	group := router.Group("/api/games/:id/platform-config")
	group.GET("", h.Get)
	group.PUT("", h.Update)
}

// Get 查询平台配置
func (h *GamePlatformConfigHandler) Get(c *gin.Context) {
	cfg, err := h.useCase.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, cfg)
}

type updatePlatformConfigRequest struct {
	Config map[string]string `json:"config"`
}

// Update 保存平台配置（仅 control=platform 的 key 允许）
func (h *GamePlatformConfigHandler) Update(c *gin.Context) {
	var req updatePlatformConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}
	if req.Config == nil {
		req.Config = map[string]string{}
	}
	operator := c.GetString("operator")
	if operator == "" {
		operator = "admin"
	}
	cfg, err := h.useCase.Update(c.Request.Context(), c.Param("id"), req.Config, operator)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, cfg)
}
