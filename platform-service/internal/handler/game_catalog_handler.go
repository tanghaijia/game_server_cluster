package handler

import (
	"errors"
	"net/http"

	"platform-service/internal/biz"
	"platform-service/internal/client/controller"

	"github.com/gin-gonic/gin"
)

// GameCatalogHandler 游戏目录（多游戏平台，见 docs/multi-game-platform-design.md）
type GameCatalogHandler struct {
	gameCatalog *biz.GameCatalogUseCase
}

func NewGameCatalogHandler(uc *biz.GameCatalogUseCase) *GameCatalogHandler {
	return &GameCatalogHandler{gameCatalog: uc}
}

// RegisterRoutes 注册游戏目录路由
func (h *GameCatalogHandler) RegisterRoutes(router *gin.Engine, auth gin.HandlerFunc) {
	router.GET("/api/games", auth, h.ListGames)
	router.GET("/api/games/:gameId", auth, h.GetGame)
}

// ListGames 聚合游戏列表：管理员见全部（含未启用），用户仅见 enabled
func (h *GameCatalogHandler) ListGames(c *gin.Context) {
	games, err := h.gameCatalog.ListGames(c.Request.Context(), isAdmin(c))
	if err != nil {
		if errors.Is(err, controller.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "games not found"});
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": "controller 不可达或返回错误: " + err.Error()});
		return
	}
	c.JSON(http.StatusOK, gin.H{"games": games})
}

// GetGame 单个游戏详情
func (h *GameCatalogHandler) GetGame(c *gin.Context) {
	game, err := h.gameCatalog.GetGame(c.Request.Context(), c.Param("gameId"), isAdmin(c))
	if err != nil {
		if errors.Is(err, controller.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "game not found"});
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": "controller 不可达或返回错误: " + err.Error()});
		return
	}
	c.JSON(http.StatusOK, game)
}
