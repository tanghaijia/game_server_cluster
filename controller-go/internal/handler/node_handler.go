package handler

import (
	"errors"
	"net/http"

	"controller-go/internal/biz"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// NodeHandler 提供服务器节点（Node）相关的 HTTP 接口
type NodeHandler struct {
	nodeUseCase *biz.NodeUseCase
}

func NewNodeHandler(uc *biz.NodeUseCase) *NodeHandler {
	return &NodeHandler{nodeUseCase: uc}
}

// RegisterRoutes 注册 Node 相关的路由
func (h *NodeHandler) RegisterRoutes(router *gin.Engine) {
	group := router.Group("/api/nodes")
	group.POST("", h.CreateNode)
	group.GET("", h.ListNodes)
	group.GET("/:id", h.GetNode)
}

type createNodeRequest struct {
	IP string `json:"ip"`
}

// CreateNode 新建节点
func (h *NodeHandler) CreateNode(c *gin.Context) {
	var req createNodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}
	if req.IP == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ip is required"})
		return
	}

	node, err := h.nodeUseCase.CreateNode(req.IP)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, node)
}

// ListNodes 列出全部节点
func (h *NodeHandler) ListNodes(c *gin.Context) {
	nodes, err := h.nodeUseCase.ListNodes(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"nodes": nodes})
}

// GetNode 按 id 查询节点
func (h *NodeHandler) GetNode(c *gin.Context) {
	node, err := h.nodeUseCase.GetNode(c.Request.Context(), c.Param("id"))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "node not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, node)
}

// NodeAgentHandler 提供 NodeAgent 相关的 HTTP 接口
type NodeAgentHandler struct {
	nodeAgentUseCase *biz.NodeAgentUseCase
}

func NewNodeAgentHandler(uc *biz.NodeAgentUseCase) *NodeAgentHandler {
	return &NodeAgentHandler{nodeAgentUseCase: uc}
}

// RegisterRoutes 注册 NodeAgent 相关的路由
func (h *NodeAgentHandler) RegisterRoutes(router *gin.Engine) {
	group := router.Group("/api/node-agents")
	group.POST("", h.CreateNodeAgent)
	group.GET("", h.ListNodeAgents)
	group.GET("/health", h.ListNodeAgentHealth)
	group.POST("/:id/enable", h.EnableNodeAgent)
	group.POST("/:id/disable", h.DisableNodeAgent)
}

type createNodeAgentRequest struct {
	Name   string `json:"name"`
	NodeID string `json:"node_id"`
	Port   int32  `json:"port"`
}

// CreateNodeAgent 新建 node_agent（默认 port 9090、状态 Enabled）
func (h *NodeAgentHandler) CreateNodeAgent(c *gin.Context) {
	var req createNodeAgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}
	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}

	agent, err := h.nodeAgentUseCase.CreateNodeAgent(c.Request.Context(), req.Name, req.NodeID, req.Port)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, agent)
}

// ListNodeAgentHealth 列出全部 node_agent 存活状态（管理员查看节点健康）
func (h *NodeAgentHandler) ListNodeAgentHealth(c *gin.Context) {
	health, err := h.nodeAgentUseCase.ListNodeAgentHealth(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()}); return
	}
	c.JSON(http.StatusOK, gin.H{"node_agents": health})
}

// ListNodeAgents 列出全部 node_agent
func (h *NodeAgentHandler) ListNodeAgents(c *gin.Context) {
	agents, err := h.nodeAgentUseCase.ListNodeAgents(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"node_agents": agents})
}

// EnableNodeAgent 启用 node_agent（进入调度与缓存循环的候选池）
func (h *NodeAgentHandler) EnableNodeAgent(c *gin.Context) {
	h.setEnabled(c, true)
}

// DisableNodeAgent 停用 node_agent（退出调度与缓存循环）
func (h *NodeAgentHandler) DisableNodeAgent(c *gin.Context) {
	h.setEnabled(c, false)
}

func (h *NodeAgentHandler) setEnabled(c *gin.Context, enabled bool) {
	agent, err := h.nodeAgentUseCase.SetEnabled(c.Request.Context(), c.Param("id"), enabled)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "node_agent not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, agent)
}
