package handler

import (
	"net/http"

	"controller-go/internal/biz"

	"github.com/gin-gonic/gin"
)

// CredentialPoolHandler 外部受限凭证池（M8，§3.6.5）。
// 供 admin 录入/管理（platform-service 透传 /api/admin/games/:id/credentials）。
type CredentialPoolHandler struct {
	useCase *biz.CredentialUseCase
}

func NewCredentialPoolHandler(uc *biz.CredentialUseCase) *CredentialPoolHandler {
	return &CredentialPoolHandler{useCase: uc}
}

func (h *CredentialPoolHandler) RegisterRoutes(router *gin.Engine) {
	group := router.Group("/api/games/:id/credentials")
	group.GET("", h.List)
	group.POST("", h.Create)
	group.GET("/types", h.ListTypes)
	group.DELETE("/:credentialId", h.Delete)
	group.POST("/:credentialId/force-release", h.ForceRelease)
}

// ListTypes 该游戏已声明的凭证类型（adapter.toml [[credentials]].pool 去重）——
// resource_type 是枚举，前端下拉选项来源
func (h *CredentialPoolHandler) ListTypes(c *gin.Context) {
	types, err := h.useCase.DeclaredTypes(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"game_id": c.Param("id"), "resource_types": types})
}

// List 列出凭证（?resource_type= 过滤）
func (h *CredentialPoolHandler) List(c *gin.Context) {
	rows, err := h.useCase.List(c.Request.Context(), c.Param("id"), c.Query("resource_type"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"game_id": c.Param("id"), "credentials": rows})
}

type createCredentialsRequest struct {
	ResourceType string   `json:"resource_type"`
	Secrets      []string `json:"secrets"`
	Remark       string   `json:"remark"`
}

// Create 批量录入凭证
func (h *CredentialPoolHandler) Create(c *gin.Context) {
	var req createCredentialsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}
	if req.ResourceType == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "resource_type is required"})
		return
	}
	n, err := h.useCase.Create(c.Request.Context(), c.Param("id"), req.ResourceType, req.Secrets, req.Remark)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"created": n})
}

// Delete 删除凭证（in_use 拒绝）
func (h *CredentialPoolHandler) Delete(c *gin.Context) {
	if err := h.useCase.Delete(c.Request.Context(), c.Param("credentialId")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

// ForceRelease 强制释放（orphan → available）
func (h *CredentialPoolHandler) ForceRelease(c *gin.Context) {
	if err := h.useCase.ForceRelease(c.Request.Context(), c.Param("credentialId")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "released"})
}
