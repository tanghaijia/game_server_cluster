package handler

import (
	"errors"
	"net/http"

	"platform-service/internal/client/controller"

	"github.com/gin-gonic/gin"
)

// AdminHandler 管理员管理接口：代理转发到 controller-go（ADR-0001：前端只连 platform-service）
type AdminHandler struct {
	controller *controller.Client
}

func NewAdminHandler(cc *controller.Client) *AdminHandler {
	return &AdminHandler{controller: cc}
}

// RegisterRoutes 注册管理员路由（全部需登录 + 管理员）
func (h *AdminHandler) RegisterRoutes(router *gin.Engine, auth gin.HandlerFunc) {
	group := router.Group("/api/admin")
	group.Use(auth, RequireAdmin())

	// 节点
	group.GET("/nodes", h.ListNodes)
	group.POST("/nodes", h.CreateNode)
	group.GET("/nodes/:id", h.GetNode)

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
	if err := h.controller.DeleteGame(c.Request.Context(), c.Param("id")); err != nil {
		fail(c, err); return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

// ------------------------- SteamBranch -------------------------

func (h *AdminHandler) ListBranches(c *gin.Context) {
	branches, err := h.controller.ListBranches(c.Request.Context(), c.Param("id"))
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
