package handler

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"controller-go/internal/biz"

	"github.com/gin-gonic/gin"
)

// AgentReleaseHandler node_agent 发布版本管理（P1，docs/node-agent-upgrade-design.md §4.2）
type AgentReleaseHandler struct {
	uc *biz.AgentReleaseUseCase
}

func NewAgentReleaseHandler(uc *biz.AgentReleaseUseCase) *AgentReleaseHandler {
	return &AgentReleaseHandler{uc: uc}
}

// RegisterRoutes 注册发布清单路由（/api/node-agents/releases）
func (h *AgentReleaseHandler) RegisterRoutes(router *gin.Engine) {
	group := router.Group("/api/node-agents/releases")
	group.POST("", h.Register)          // multipart 上传（version/os/arch/note/file）
	group.GET("", h.List)               // 清单列表
	group.GET("/:id/download", h.Download) // 下载二进制（admin 或 node_agent 更新拉取）
}

type releaseForm struct {
	Version string
	OS      string
	Arch    string
	Note    string
}

// Register 上传并登记新版 node_agent 二进制（multipart/form-data）
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

// Download 下载二进制（流式）
func (h *AgentReleaseHandler) Download(c *gin.Context) {
	release, rc, err := h.uc.OpenBinary(c.Request.Context(), c.Param("id"))
	if err != nil {
		if errors.Is(err, biz.ErrReleaseNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "release not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rc.Close()

	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition", "attachment; filename=\"node-agent-"+release.Version+"\"")
	c.Header("X-Release-SHA256", release.SHA256)
	c.Header("X-Release-Size", strconv.FormatInt(release.SizeBytes, 10))
	c.Status(http.StatusOK)
	// 流式拷贝（gin 支持直接写 c.Writer）
	if _, err := io.Copy(c.Writer, rc); err != nil {
		// 响应已开始，无法改状态码；记录即可（gin 默认吞掉）
		return
	}
}
