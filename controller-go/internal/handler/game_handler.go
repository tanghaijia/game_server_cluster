package handler

import (
	"errors"
	"net/http"

	"controller-go/internal/biz"
	"controller-go/internal/client/assetservice"
	assetservicev1 "controller-go/internal/third/assetservice/v1"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GameHandler 提供 Game 相关的 HTTP 接口（增删改查，写操作同步 asset_service）
type GameHandler struct {
	gameUseCase *biz.GameUseCase
	assetClient *assetservice.AssetServiceFaceClient
}

func NewGameHandler(uc *biz.GameUseCase, assetClient *assetservice.AssetServiceFaceClient) *GameHandler {
	return &GameHandler{gameUseCase: uc, assetClient: assetClient}
}

// RegisterRoutes 注册 Game 相关的路由
func (h *GameHandler) RegisterRoutes(router *gin.Engine) {
	group := router.Group("/api/games")
	group.POST("", h.CreateGame)
	group.GET("", h.ListGames)
	group.GET("/:id", h.GetGame)
	group.PUT("/:id", h.UpdateGame)
	group.DELETE("/:id", h.DeleteGame)

	// game_build 管理（资产版本）
	group.GET("/:id/builds", h.ListGameBuilds)
	group.POST("/:id/builds", h.RegisterGameBuild)
	group.GET("/:id/builds/:buildId", h.GetGameBuild)
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

// ------------------------- game_build 管理（资产版本） -------------------------

// ListGameBuilds 列出某游戏的构建版本（?channel= 过滤）
func (h *GameHandler) ListGameBuilds(c *gin.Context) {
	gameID := c.Param("id")
	resp, err := h.assetClient.ListGameBuilds(c.Request.Context(), &assetservicev1.ListGameBuildsRequest{
		GameId:  gameID,
		Channel: optionalString(c.Query("channel")),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()}); return
	}
	c.JSON(http.StatusOK, gin.H{"game_id": gameID, "builds": resp.Builds})
}

type registerGameBuildRequest struct {
	BuildID            string `json:"build_id"`
	GameID             string `json:"game_id"`
	Channel            string `json:"channel"`
	AdapterID          string `json:"adapter_id"`
	AdapterVersion     string `json:"adapter_version"`
	UpstreamVersion    string `json:"upstream_version"`
	ArtifactURI        string `json:"artifact_uri"`
	ArtifactImageName  string `json:"artifact_image_name"`
	ArtifactImageTag   string `json:"artifact_image_tag"`
}

// RegisterGameBuild 注册新构建版本
func (h *GameHandler) RegisterGameBuild(c *gin.Context) {
	var req registerGameBuildRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()}); return
	}
	if req.BuildID == "" || req.GameID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "build_id and game_id are required"}); return
	}

	resp, err := h.assetClient.RegisterGameBuild(c.Request.Context(), &assetservicev1.RegisterGameBuildRequest{
		Build: &assetservicev1.GameBuild{
			BuildId:            req.BuildID,
			Game:               &assetservicev1.Game{Id: req.GameID},
			Channel:            optionalString(req.Channel),
			AdapterId:          req.AdapterID,
			AdapterVersion:     optionalString(req.AdapterVersion),
			UpstreamVersion:    optionalString(req.UpstreamVersion),
			ArtifactUri:        optionalString(req.ArtifactURI),
			ArtifactImageName:  optionalString(req.ArtifactImageName),
			ArtifactImageTag:   optionalString(req.ArtifactImageTag),
			// asset_service 的 rpc 层要求 status 非 0；新注册默认 Available（asset_service 内部也会置 Available）
			Status:             assetservicev1.BuildStatus_BUILD_STATUS_AVAILABLE,
		},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()}); return
	}
	c.JSON(http.StatusCreated, resp.Build)
}

// GetGameBuild 构建详情
func (h *GameHandler) GetGameBuild(c *gin.Context) {
	resp, err := h.assetClient.GetGameBuild(c.Request.Context(), &assetservicev1.GetGameBuildRequest{
		BuildId: c.Param("buildId"),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()}); return
	}
	c.JSON(http.StatusOK, resp.Build)
}

// optionalString 空串 → nil（proto optional 字段）
func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
