package handler

import (
	"errors"
	"net/http"

	"controller-go/internal/biz"

	"github.com/gin-gonic/gin"
)

// AgentReleaseHandler node_agent 发布版本管理（P1 原始 + P2 asset_service 托管，
// docs/agent-release-asset-service-redesign.md）：上传经 asset_service 写对象存储，
// controller 只登记清单（不再提供本地下载端点）。
type AgentReleaseHandler struct {
	uc *biz.AgentReleaseUseCase
}

func NewAgentReleaseHandler(uc *biz.AgentReleaseUseCase) *AgentReleaseHandler {
	return &AgentReleaseHandler{uc: uc}
}

// RegisterRoutes 注册发布清单路由（/api/node-agents/releases）
func (h *AgentReleaseHandler) RegisterRoutes(router *gin.Engine) {
	group := router.Group("/api/node-agents/releases")
	group.POST("", h.Register) // multipart 上传（version/os/arch/note/file）
	group.GET("", h.List)      // 清单列表
}

type releaseForm struct {
	Version string
	OS      string
	Arch    string
	Note    string
}

// Register 上传并登记新版 node_agent 二进制（multipart/form-data → asset_service 流式写对象存储）
func (h *AgentReleaseHandler) Register(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 file 字段（multipart 文件）"})
		return
	}
	f, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "open upload: " + err.Error()})
		return
	}
	defer f.Close()

	release, err := h.uc.Register(c.Request.Context(), biz.RegisterParams{
		Version: c.PostForm("version"),
		OS:      c.PostForm("os"),
		Arch:    c.PostForm("arch"),
		Note:    c.PostForm("note"),
		ByUser:  c.GetHeader("X-Admin-User"), // platform-service admin 鉴权后透传
		Body:    f,
	})
	if err != nil {
		code := http.StatusInternalServerError
		switch {
		case errors.Is(err, biz.ErrReleaseInvalidVersion), errors.Is(err, biz.ErrReleaseInvalidTarget):
			code = http.StatusBadRequest
		case errors.Is(err, biz.ErrReleaseNotFound):
			code = http.StatusNotFound
		}
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, release)
}

// List 发布清单
func (h *AgentReleaseHandler) List(c *gin.Context) {
	releases, err := h.uc.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"releases": releases})
}
