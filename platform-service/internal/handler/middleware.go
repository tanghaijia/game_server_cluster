package handler

import (
	"net/http"
	"strings"

	"platform-service/internal/auth"
	"platform-service/internal/entity"

	"github.com/gin-gonic/gin"
)

const (
	ctxKeyUserID   = "user_id"
	ctxKeyUserRole = "user_role"
)

// AuthRequired 校验 Bearer access token，并把 user_id / user_role 放入 context（ADR-0004）
func AuthRequired(tokens *auth.TokenManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing bearer token"})
			return
		}

		claims, err := tokens.Parse(strings.TrimPrefix(header, "Bearer "))
		if err != nil || claims.TokenType != auth.TokenTypeAccess {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			return
		}

		c.Set(ctxKeyUserID, claims.UserID)
		c.Set(ctxKeyUserRole, claims.Role)
		c.Next()
	}
}

// RequireAdmin 在 AuthRequired 之后使用：非管理员直接 403
func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if CurrentUserRole(c) != int(entity.RoleAdmin) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin permission required"})
			return
		}
		c.Next()
	}
}

// CurrentUserID 返回当前登录用户 ID
func CurrentUserID(c *gin.Context) string {
	id, _ := c.Get(ctxKeyUserID)
	s, _ := id.(string)
	return s
}

// CurrentUserRole 返回当前登录用户角色（0=user 1=admin）
func CurrentUserRole(c *gin.Context) int {
	role, _ := c.Get(ctxKeyUserRole)
	r, _ := role.(int)
	return r
}

// isAdmin 便捷判断
func isAdmin(c *gin.Context) bool {
	return CurrentUserRole(c) == int(entity.RoleAdmin)
}
