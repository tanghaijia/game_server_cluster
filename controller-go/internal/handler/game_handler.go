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
	// 配置 schema（000025）：前端表单生成 / 实例配置校验数据源
	group.GET("/:id/config-schema", h.GetConfigSchema)
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

// GetConfigSchema 游戏配置 schema（000025，前端表单生成 / 实例配置校验数据源）。
// 返回该游戏最新携带 schema_json 的构建（含 adapter_metadata）。
func (h *GameHandler) GetConfigSchema(c *gin.Context) {
	gameID := c.Param("id")
	resp, err := h.assetClient.ListGameBuilds(c.Request.Context(), &assetservicev1.ListGameBuildsRequest{
		GameId: gameID,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()}); return
	}
	for _, b := range resp.Builds {
		if b.GetSchemaJson() != "" {
			c.JSON(http.StatusOK, gin.H{
				"game_id":          gameID,
				"build_id":         b.BuildId,
				"schema_json":      b.GetSchemaJson(),
				"adapter_metadata": b.GetAdapterMetadata(),
			})
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "game has no build with config schema"})
}

type registerGameBuildRequest struct {
	// 迭代注册（增量语义）：build_id 由系统按 {game_id}-{channel}-{tag} 生成，不接受手填；
	// 除 artifact_image_tag 外字段均可省略，未提供的字段从 base_build_id（缺省 = 同
	// channel 最新 Available）继承。
	GameID            string `json:"game_id"`
	Channel           string `json:"channel"`
	BaseBuildID       string `json:"base_build_id"`
	AdapterID         string `json:"adapter_id"`
	AdapterVersion    string `json:"adapter_version"`
	UpstreamVersion   string `json:"upstream_version"`
	ArtifactURI       string `json:"artifact_uri"`
	ArtifactImageName string `json:"artifact_image_name"`
	ArtifactImageTag  string `json:"artifact_image_tag"`
	// 收敛模型（M5）：适配器元数据/schema 随构建注册携带（gen_manifest.py 产物），
	// 不再有独立 adapter 实体；以下字段可选，缺省时 build 无配置能力
	AdapterMetadata *assetservicev1.AdapterMetadata `json:"adapter_metadata,omitempty"`
	SchemaJSON      string                          `json:"schema_json,omitempty"`
}

// RegisterGameBuild 注册新构建版本（增量迭代：只传需要更新的字段）
func (h *GameHandler) RegisterGameBuild(c *gin.Context) {
	var req registerGameBuildRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()}); return
	}
	if req.GameID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "game_id is required"}); return
	}
	if req.ArtifactImageTag == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "artifact_image_tag is required（新版本身份）"}); return
	}

	resp, err := h.assetClient.RegisterGameBuild(c.Request.Context(), &assetservicev1.RegisterGameBuildRequest{
		Build: &assetservicev1.GameBuild{
			Game:               &assetservicev1.Game{Id: req.GameID},
			Channel:            optionalString(req.Channel),
			AdapterId:          req.AdapterID,
			AdapterVersion:     optionalString(req.AdapterVersion),
			UpstreamVersion:    optionalString(req.UpstreamVersion),
			ArtifactUri:        optionalString(req.ArtifactURI),
			ArtifactImageName:  optionalString(req.ArtifactImageName),
			ArtifactImageTag:   optionalString(req.ArtifactImageTag),
			AdapterMetadata:    req.AdapterMetadata,
			SchemaJson:         optionalString(req.SchemaJSON),
			// asset_service 的 rpc 层要求 status 非 0；新注册默认 Available（asset_service 内部也会置 Available）
			Status:             assetservicev1.BuildStatus_BUILD_STATUS_AVAILABLE,
		},
		BaseBuildId: optionalString(req.BaseBuildID),
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
