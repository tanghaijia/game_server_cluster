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

// FileSessionHandler 实例文件会话（M2，见 docs/file-manager-design.md）：
// 查询实例所在 node_agent，签发短效 JWT 供浏览器直连 node_agent 文件服务。
type FileSessionHandler struct {
	instanceUseCase *biz.GameInstanceUseCase
	nodeAgentRepo   repository.NodeAgentRepository
	nodeRepo        repository.NodeRepository
	issuer          *biz.FileSessionIssuer
	filePortOffset  int
}

func NewFileSessionHandler(
	instanceUseCase *biz.GameInstanceUseCase,
	nodeAgentRepo repository.NodeAgentRepository,
	nodeRepo repository.NodeRepository,
	issuer *biz.FileSessionIssuer,
	filePortOffset int,
) *FileSessionHandler {
	return &FileSessionHandler{
		instanceUseCase: instanceUseCase,
		nodeAgentRepo:   nodeAgentRepo,
		nodeRepo:        nodeRepo,
		issuer:          issuer,
		filePortOffset:  filePortOffset,
	}
}

// RegisterRoutes 注册文件会话路由
func (h *FileSessionHandler) RegisterRoutes(router *gin.Engine) {
	router.POST("/api/game-instances/:id/file-session", h.FileSession)
}

// FileSession 为实例签发文件会话
func (h *FileSessionHandler) FileSession(c *gin.Context) {
	id := c.Param("id")
	instance, err := h.instanceUseCase.GetGameInstance(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "instance not found"});
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()});
		return
	}

	if instance.NodeAgentID == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "instance has no node_agent, dispatch it first"});
		return
	}

	nodeAgent, err := h.nodeAgentRepo.GetByID(c.Request.Context(), *instance.NodeAgentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "load node_agent: " + err.Error()});
		return
	}
	node, err := h.nodeRepo.GetByID(nodeAgent.NodeId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "load node: " + err.Error()});
		return
	}

	filePort := nodeAgent.Port + int32(h.filePortOffset)
	baseURL := fmt.Sprintf("http://%s:%d", node.Ip, filePort)
	token, expiresAt, err := h.issuer.Issue(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "issue token: " + err.Error()});
		return
	}

	c.JSON(http.StatusOK, biz.FileSession{
		BaseURL:    baseURL,
		Token:      token,
		InstanceID: id,
		DataRoot:   "/data",
		ExpiresAt:  expiresAt,
	})
}
