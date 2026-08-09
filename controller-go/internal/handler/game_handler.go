package handler

import (
	"errors"
	"net/http"

	"controller-go/internal/biz"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GameHandler 提供 Game 相关的 HTTP 接口（增删改查，写操作同步 asset_service）
type GameHandler struct {
	gameUseCase *biz.GameUseCase
}

func NewGameHandler(uc *biz.GameUseCase) *GameHandler {
	return &GameHandler{gameUseCase: uc}
}

// RegisterRoutes 注册 Game 相关的路由
func (h *GameHandler) RegisterRoutes(router *gin.Engine) {
	group := router.Group("/api/games")
	group.POST("", h.CreateGame)
	group.GET("", h.ListGames)
	group.GET("/:id", h.GetGame)
	group.PUT("/:id", h.UpdateGame)
	group.DELETE("/:id", h.DeleteGame)
}

type gameRequest struct {
	Name  string `json:"name"`
	AppID string `json:"app_id"`
}

// CreateGame 新建 Game（写操作同步 asset_service）
func (h *GameHandler) CreateGame(c *gin.Context) {
	var req gameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}
	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}

	game, err := h.gameUseCase.CreateGame(c.Request.Context(), req.Name, req.AppID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, game)
}

// GetGame 按 id 查询（读本地库）
func (h *GameHandler) GetGame(c *gin.Context) {
	game, err := h.gameUseCase.GetGame(c.Request.Context(), c.Param("id"))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "game not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, game)
}

// UpdateGame 更新 Game 的 name / app_id（写操作同步 asset_service）
func (h *GameHandler) UpdateGame(c *gin.Context) {
	var req gameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}
	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}

	game, err := h.gameUseCase.UpdateGame(c.Request.Context(), c.Param("id"), req.Name, req.AppID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, game)
}

// DeleteGame 删除 Game（写操作同步 asset_service）
func (h *GameHandler) DeleteGame(c *gin.Context) {
	if err := h.gameUseCase.DeleteGame(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

// ListGames 列出全部 Game（读本地库）
func (h *GameHandler) ListGames(c *gin.Context) {
	games, err := h.gameUseCase.ListGames(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"games": games})
}
