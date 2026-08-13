package handler

import (
	"net/http"

	"platform-service/internal/auth"
	"platform-service/internal/biz"

	"github.com/gin-gonic/gin"
)

// AuthHandler 提供登录接口
type AuthHandler struct {
	userUseCase *biz.UserUseCase
	tokens      *auth.TokenManager
}

func NewAuthHandler(uc *biz.UserUseCase, tokens *auth.TokenManager) *AuthHandler {
	return &AuthHandler{userUseCase: uc, tokens: tokens}
}

// RegisterRoutes 注册认证路由
func (h *AuthHandler) RegisterRoutes(router *gin.Engine) {
	router.POST("/api/auth/login", h.Login)
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Login 用户名密码登录，签发 access + refresh token（ADR-0004）
func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}

	user, err := h.userUseCase.VerifyPassword(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid username or password"})
		return
	}

	access, refresh, err := h.tokens.Issue(user.ID, int(user.Role))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"access_token":  access,
		"refresh_token": refresh,
		"user": gin.H{
			"id":       user.ID,
			"username": user.Username,
			"role":     user.Role,
		},
	})
}

// （AuthRequired / RequireAdmin 已移至 middleware.go）
