package handler

import (
	"testing"

	"github.com/gin-gonic/gin"
)

// 临时冒烟测试：确认所有 handler 的路由能无冲突注册
func TestAllRoutesRegister(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewGameInstanceHandler(nil).RegisterRoutes(r)
	NewGameHandler(nil).RegisterRoutes(r)
	NewNodeHandler(nil).RegisterRoutes(r)
	NewNodeAgentHandler(nil).RegisterRoutes(r)
	NewGameCacheHandler(nil).RegisterRoutes(r)
	NewDebugHandler(nil).RegisterRoutes(r)

	if len(r.Routes()) == 0 {
		t.Fatal("no routes registered")
	}
	for _, rt := range r.Routes() {
		t.Logf("%s %s", rt.Method, rt.Path)
	}
}
