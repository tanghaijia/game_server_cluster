package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RegisterHealthRoutes 注册健康检查路由
func RegisterHealthRoutes(router *gin.Engine) {
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
}
