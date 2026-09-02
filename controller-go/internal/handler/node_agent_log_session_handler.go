package handler

import (
	"errors"
	"fmt"
	"net/http"

	"controller-go/internal/biz"
	"controller-go/internal/repository"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// NodeAgentLogSessionHandler node_agent 日志会话（P2，见 docs/node-agent-logging-design.md §4.2）：
// 查询 node_agent 所在 node，签发短效 agent_logs JWT 供浏览器直连其 HTTP 日志端点。
type NodeAgentLogSessionHandler struct {
	nodeAgentRepo repository.NodeAgentRepository
	nodeRepo      repository.NodeRepository
	issuer        *biz.FileSessionIssuer
	filePortOffset int
}

func NewNodeAgentLogSessionHandler(
	nodeAgentRepo repository.NodeAgentRepository,
	nodeRepo repository.NodeRepository,
	issuer *biz.FileSessionIssuer,
	filePortOffset int,
) *NodeAgentLogSessionHandler {
	return &NodeAgentLogSessionHandler{
		nodeAgentRepo:  nodeAgentRepo,
		nodeRepo:       nodeRepo,
		issuer:         issuer,
		filePortOffset: filePortOffset,
	}
}

// RegisterRoutes 注册 node_agent 日志会话路由
func (h *NodeAgentLogSessionHandler) RegisterRoutes(router *gin.Engine) {
	router.POST("/api/node-agents/:id/log-session", h.LogSession)
}

// LogSession 为 node_agent 签发日志会话
func (h *NodeAgentLogSessionHandler) LogSession(c *gin.Context) {
	id := c.Param("id")
	nodeAgent, err := h.nodeAgentRepo.GetByID(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "node_agent not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "load node_agent: " + err.Error()})
		return
	}
	if nodeAgent.NodeId == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "node_agent " + nodeAgent.ID + " has no node_id assigned, set node_id first"})
		return
	}
	node, err := h.nodeRepo.GetByID(nodeAgent.NodeId)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "node " + nodeAgent.NodeId + " not found for node_agent " + nodeAgent.ID})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "load node: " + err.Error()})
		return
	}

	filePort := nodeAgent.Port + int32(h.filePortOffset)
	baseURL := fmt.Sprintf("http://%s:%d", node.Ip, filePort)
	token, expiresAt, err := h.issuer.IssueForAgent(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "issue token: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, biz.AgentLogSession{
		BaseURL:   baseURL,
		Token:     token,
		AgentID:   id,
		ExpiresAt: expiresAt,
	})
}
