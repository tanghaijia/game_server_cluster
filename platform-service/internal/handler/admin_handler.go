package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"platform-service/internal/biz"
	"platform-service/internal/client/controller"
	"platform-service/internal/entity"

	"github.com/gin-gonic/gin"
)

// AdminHandler 管理员管理接口：代理转发到 controller-go（ADR-0001：前端只连 platform-service）
type AdminHandler struct {
	controller  *controller.Client
	gameCatalog *biz.GameCatalogUseCase
}

func NewAdminHandler(cc *controller.Client, gc *biz.GameCatalogUseCase) *AdminHandler {
	return &AdminHandler{controller: cc, gameCatalog: gc}
}

// RegisterRoutes 注册管理员路由（全部需登录 + 管理员）
func (h *AdminHandler) RegisterRoutes(router *gin.Engine, auth gin.HandlerFunc) {
	group := router.Group("/api/admin")
	group.Use(auth, RequireAdmin())

	// 节点
	group.GET("/nodes", h.ListNodes)
	group.POST("/nodes", h.CreateNode)
	group.GET("/nodes/:id", h.GetNode)
	group.PUT("/nodes/:id", h.UpdateNode)
	group.DELETE("/nodes/:id", h.DeleteNode)

	// node_agent
	group.GET("/node-agents", h.ListNodeAgents)
	group.POST("/node-agents", h.CreateNodeAgent)
	group.POST("/node-agents/:id/enable", h.EnableNodeAgent)
	group.POST("/node-agents/:id/disable", h.DisableNodeAgent)

	// 游戏
	group.GET("/games", h.ListGames)
	group.POST("/games", h.CreateGame)
	group.GET("/games/:id", h.GetGame)
	group.PUT("/games/:id", h.UpdateGame)
	group.DELETE("/games/:id", h.DeleteGame)

	// 游戏容器配置（端口/资源默认值，图形化配置）
	group.GET("/games/:id/container-config", h.GetContainerConfig)
	group.PUT("/games/:id/container-config", h.UpdateContainerConfig)

	// 调度观测（转发 controller /api/observe/*，管理员鉴权）
	group.Any("/observe/*path", h.ObserveForward)

	// 游戏资料（多游戏平台）
	group.PUT("/games/:id/profile", h.UpdateGameProfile)
	group.POST("/games/:id/icon", h.UploadGameIcon)

	// game_build 管理（资产版本）
	group.GET("/games/:id/builds", h.ListGameBuilds)
	group.POST("/games/:id/builds", h.RegisterGameBuild)
	group.GET("/games/:id/builds/:buildId", h.GetGameBuild)

	// 文件会话（管理员可对任意实例）
	group.POST("/instances/:instanceId/file-session", h.InstanceFileSession)

	// Steam 分支
	group.GET("/games/:id/branches", h.ListBranches)
	group.POST("/games/:id/branches/sync", h.SyncBranches)
	group.POST("/games/:id/branches/:branch/cache", h.UpdateBranchCache)
}

// fail 统一错误响应：controller 404 映射为 404，其余 500
func fail(c *gin.Context, err error) {
	if errors.Is(err, controller.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"});
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
}

// ------------------------- Node -------------------------

func (h *AdminHandler) ListNodes(c *gin.Context) {
	nodes, err := h.controller.ListNodes(c.Request.Context())
	if err != nil {
		fail(c, err); return
	}
	c.JSON(http.StatusOK, gin.H{"nodes": nodes})
}

type createNodeRequest struct {
	IP string `json:"ip"`
}

func (h *AdminHandler) CreateNode(c *gin.Context) {
	var req createNodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()}); return
	}
	if req.IP == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ip is required"}); return
	}
	node, err := h.controller.CreateNode(c.Request.Context(), req.IP)
	if err != nil {
		fail(c, err); return
	}
	c.JSON(http.StatusCreated, node)
}

func (h *AdminHandler) GetNode(c *gin.Context) {
	node, err := h.controller.GetNode(c.Request.Context(), c.Param("id"))
	if err != nil {
		fail(c, err); return
	}
	c.JSON(http.StatusOK, node)
}

// UpdateNode 更新节点配置（非 nil 字段生效）
func (h *AdminHandler) UpdateNode(c *gin.Context) {
	var req controller.NodeUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()}); return
	}
	node, err := h.controller.UpdateNode(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		fail(c, err); return
	}
	c.JSON(http.StatusOK, node)
}

// DeleteNode 删除节点（controller 对仍被 node_agent 引用的节点返回 409）
func (h *AdminHandler) DeleteNode(c *gin.Context) {
	if err := h.controller.DeleteNode(c.Request.Context(), c.Param("id")); err != nil {
		if errors.Is(err, controller.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

// ------------------------- NodeAgent -------------------------

func (h *AdminHandler) ListNodeAgents(c *gin.Context) {
	agents, err := h.controller.ListNodeAgents(c.Request.Context())
	if err != nil {
		fail(c, err); return
	}
	c.JSON(http.StatusOK, gin.H{"node_agents": agents})
}

type createNodeAgentRequest struct {
	Name   string `json:"name"`
	NodeID string `json:"node_id"`
	Port   int32  `json:"port"`
}

func (h *AdminHandler) CreateNodeAgent(c *gin.Context) {
	var req createNodeAgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()}); return
	}
	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"}); return
	}
	agent, err := h.controller.CreateNodeAgent(c.Request.Context(), req.Name, req.NodeID, req.Port)
	if err != nil {
		fail(c, err); return
	}
	c.JSON(http.StatusCreated, agent)
}

func (h *AdminHandler) EnableNodeAgent(c *gin.Context) { h.setNodeAgentEnabled(c, true) }
func (h *AdminHandler) DisableNodeAgent(c *gin.Context) { h.setNodeAgentEnabled(c, false) }

func (h *AdminHandler) setNodeAgentEnabled(c *gin.Context, enabled bool) {
	agent, err := h.controller.SetNodeAgentEnabled(c.Request.Context(), c.Param("id"), enabled)
	if err != nil {
		fail(c, err); return
	}
	c.JSON(http.StatusOK, agent)
}

// ------------------------- Game -------------------------

func (h *AdminHandler) ListGames(c *gin.Context) {
	games, err := h.controller.ListGames(c.Request.Context())
	if err != nil {
		fail(c, err); return
	}
	c.JSON(http.StatusOK, gin.H{"games": games})
}

type createGameRequest struct {
	Name  string `json:"name"`
	AppID string `json:"app_id"`
}

func (h *AdminHandler) CreateGame(c *gin.Context) {
	var req createGameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()}); return
	}
	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"}); return
	}
	game, err := h.controller.CreateGame(c.Request.Context(), req.Name, req.AppID)
	if err != nil {
		fail(c, err); return
	}
	c.JSON(http.StatusCreated, game)
}

func (h *AdminHandler) GetGame(c *gin.Context) {
	game, err := h.controller.GetGame(c.Request.Context(), c.Param("id"))
	if err != nil {
		fail(c, err); return
	}
	c.JSON(http.StatusOK, game)
}

func (h *AdminHandler) UpdateGame(c *gin.Context) {
	var req createGameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()}); return
	}
	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"}); return
	}
	game, err := h.controller.UpdateGame(c.Request.Context(), c.Param("id"), req.Name, req.AppID)
	if err != nil {
		fail(c, err); return
	}
	c.JSON(http.StatusOK, game)
}

func (h *AdminHandler) DeleteGame(c *gin.Context) {
	// 级联删除：controller 删实例/分支/配置 + platform 删资料 + 订单标记下架
	if err := h.gameCatalog.DeleteGame(c.Request.Context(), c.Param("id")); err != nil {
		fail(c, err); return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

// ------------------------- Game ContainerConfig -------------------------

// GetContainerConfig 获取游戏容器配置（端口/资源默认值）
func (h *AdminHandler) GetContainerConfig(c *gin.Context) {
	cfg, err := h.controller.GetContainerConfig(c.Request.Context(), c.Param("id"))
	if err != nil {
		fail(c, err); return
	}
	c.JSON(http.StatusOK, cfg)
}

// UpdateContainerConfig 更新游戏容器配置（端口片段整体替换）
func (h *AdminHandler) UpdateContainerConfig(c *gin.Context) {
	var req controller.ContainerConfigUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()}); return
	}
	cfg, err := h.controller.UpdateContainerConfig(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		fail(c, err); return
	}
	c.JSON(http.StatusOK, cfg)
}

// ------------------------- 调度观测转发 -------------------------

// ObserveForward 透传 /api/admin/observe/* 到 controller /api/observe/*（管理员鉴权已由路由组中间件完成）。
// 响应体原样透传；非 2xx 统一映射为 500（404 单独映射）。
// 注意：必须透传原始 query string（如 events?hours=24），否则 controller 读不到 hours/limit 等参数。
func (h *AdminHandler) ObserveForward(c *gin.Context) {
	sub := c.Param("path") // 如 /nodes、/nodes/1/history、/scheduler/preview
	var body any
	if c.Request.Body != nil && c.Request.ContentLength > 0 {
		b, err := io.ReadAll(c.Request.Body)
		if err == nil && len(b) > 0 {
			body = json.RawMessage(b)
		}
	}
	var out json.RawMessage
	if err := h.controller.ObserveForward(c.Request.Context(), c.Request.Method, sub, c.Request.URL.RawQuery, body, &out); err != nil {
		if errors.Is(err, controller.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", out)
}

// ------------------------- SteamBranch -------------------------

func (h *AdminHandler) ListBranches(c *gin.Context) {	branches, err := h.controller.ListBranches(c.Request.Context(), c.Param("id"))
	if err != nil {
		fail(c, err); return
	}
	c.JSON(http.StatusOK, gin.H{"game_id": c.Param("id"), "branches": branches})
}

func (h *AdminHandler) SyncBranches(c *gin.Context) {
	if err := h.controller.SyncBranches(c.Request.Context(), c.Param("id")); err != nil {
		fail(c, err); return
	}
	c.JSON(http.StatusOK, gin.H{"message": "synced"})
}

type updateBranchCacheRequest struct {
	NodeAgentID string `json:"node_agent_id"`
}

// InstanceFileSession 管理员为任意实例签发文件会话
func (h *AdminHandler) InstanceFileSession(c *gin.Context) {
	session, err := h.controller.CreateFileSession(c.Request.Context(), c.Param("instanceId"))
	if err != nil {
		fail(c, err);
		return
	}
	c.JSON(http.StatusOK, session)
}

func (h *AdminHandler) UpdateBranchCache(c *gin.Context) {
	var req updateBranchCacheRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()}); return
	}
	if req.NodeAgentID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "node_agent_id is required"}); return
	}
	if err := h.controller.UpdateBranchCache(c.Request.Context(), c.Param("id"), c.Param("branch"), req.NodeAgentID); err != nil {
		fail(c, err); return
	}
	c.JSON(http.StatusOK, gin.H{"message": "cache ok"})
}

// ------------------------- 游戏资料（多游戏平台） -------------------------

type gameProfileRequest struct {
	DisplayName string `json:"display_name"`
	IconURL     string `json:"icon_url"`
	AccentColor string `json:"accent_color"`
	Description string `json:"description"`
	Enabled     *bool  `json:"enabled"`
	SortOrder   *int   `json:"sort_order"`
}

// UpdateGameProfile 更新游戏资料（admin；不存在则创建）
func (h *AdminHandler) UpdateGameProfile(c *gin.Context) {
	var req gameProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()});
		return
	}

	updates := &entity.GameProfile{
		DisplayName: req.DisplayName,
		IconURL:     req.IconURL,
		AccentColor: req.AccentColor,
		Description: req.Description,
	}
	if req.Enabled != nil {
		updates.Enabled = *req.Enabled
	}
	if req.SortOrder != nil {
		updates.SortOrder = *req.SortOrder
	}

	prof, err := h.gameCatalog.UpdateProfile(c.Request.Context(), c.Param("id"), updates)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()});
		return
	}
	c.JSON(http.StatusOK, prof)
}

// UploadGameIcon 上传游戏图标（≤1MB，存 static/games/{gameId}.png）
func (h *AdminHandler) UploadGameIcon(c *gin.Context) {
	gameID := c.Param("id")
	if gameID == "" || strings.ContainsAny(gameID, "/\\.") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid game_id"});
		return
	}
	// 游戏必须存在（controller）
	if _, err := h.controller.GetGame(c.Request.Context(), gameID); err != nil {
		fail(c, err);
		return
	}

	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"});
		return
	}
	if file.Size > 1024*1024 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "icon too large (max 1MB)"});
		return
	}

	if err := os.MkdirAll("static/games", 0o755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()});
		return
	}
	if err := c.SaveUploadedFile(file, "static/games/"+gameID+".png"); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "save icon: " + err.Error()});
		return
	}

	prof, err := h.gameCatalog.UpdateProfile(c.Request.Context(), gameID, &entity.GameProfile{
		IconURL: "/static/games/" + gameID + ".png",
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()});
		return
	}
	c.JSON(http.StatusOK, prof)
}

// ------------------------- game_build 管理 -------------------------

// ListGameBuilds 列出某游戏构建版本（?channel= 过滤）
func (h *AdminHandler) ListGameBuilds(c *gin.Context) {
	builds, err := h.controller.ListGameBuilds(c.Request.Context(), c.Param("id"), c.Query("channel"))
	if err != nil {
		fail(c, err); return
	}
	c.JSON(http.StatusOK, gin.H{"game_id": c.Param("id"), "builds": builds})
}

type registerGameBuildRequest struct {
	BuildID           string `json:"build_id"`
	Channel           string `json:"channel"`
	AdapterID         string `json:"adapter_id"`
	AdapterVersion    string `json:"adapter_version"`
	UpstreamVersion   string `json:"upstream_version"`
	ArtifactURI       string `json:"artifact_uri"`
	ArtifactImageName string `json:"artifact_image_name"`
	ArtifactImageTag  string `json:"artifact_image_tag"`
}

// RegisterGameBuild 注册新构建
func (h *AdminHandler) RegisterGameBuild(c *gin.Context) {
	var req registerGameBuildRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()}); return
	}
	if req.BuildID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "build_id is required"}); return
	}
	strPtr := func(s string) *string {
		if s == "" { return nil }
		return &s
	}
	build, err := h.controller.RegisterGameBuild(c.Request.Context(), c.Param("id"), &controller.GameBuild{
		BuildId:           req.BuildID,
		Game:              &struct{ Id string `json:"id"` }{Id: c.Param("id")},
		Channel:           strPtr(req.Channel),
		AdapterId:         req.AdapterID,
		AdapterVersion:    strPtr(req.AdapterVersion),
		UpstreamVersion:   strPtr(req.UpstreamVersion),
		ArtifactUri:       strPtr(req.ArtifactURI),
		ArtifactImageName: strPtr(req.ArtifactImageName),
		ArtifactImageTag:  strPtr(req.ArtifactImageTag),
	})
	if err != nil {
		fail(c, err); return
	}
	c.JSON(http.StatusCreated, build)
}

// GetGameBuild 构建详情
func (h *AdminHandler) GetGameBuild(c *gin.Context) {
	build, err := h.controller.GetGameBuild(c.Request.Context(), c.Param("id"), c.Param("buildId"))
	if err != nil {
		fail(c, err); return
	}
	c.JSON(http.StatusOK, build)
}
