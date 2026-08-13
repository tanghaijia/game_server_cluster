package handler

import (
	"errors"
	"net/http"

	"platform-service/internal/biz"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// UserHandler 提供用户相关的 HTTP 接口
type UserHandler struct {
	userUseCase *biz.UserUseCase
}

func NewUserHandler(uc *biz.UserUseCase) *UserHandler {
	return &UserHandler{userUseCase: uc}
}

// RegisterRoutes 注册用户路由。
// POST（注册）开放；列表/查询需要登录（auth 中间件）。
func (h *UserHandler) RegisterRoutes(router *gin.Engine, auth gin.HandlerFunc) {
	group := router.Group("/api/users")
	group.POST("", h.CreateUser)
	group.GET("", auth, h.ListUsers)
	group.GET("/:id", auth, h.GetUser)
}

type createUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// CreateUser 创建用户
func (h *UserHandler) CreateUser(c *gin.Context) {
	var req createUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}

	user, err := h.userUseCase.CreateUser(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// 不返回密码哈希
	user.PasswordHash = ""
	c.JSON(http.StatusCreated, user)
}

// ListUsers 列出全部用户
func (h *UserHandler) ListUsers(c *gin.Context) {
	users, err := h.userUseCase.ListUsers(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	for _, u := range users {
		u.PasswordHash = ""
	}
	c.JSON(http.StatusOK, gin.H{"users": users})
}

// GetUser 按 id 查询用户
func (h *UserHandler) GetUser(c *gin.Context) {
	user, err := h.userUseCase.GetUser(c.Request.Context(), c.Param("id"))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	user.PasswordHash = ""
	c.JSON(http.StatusOK, user)
}
