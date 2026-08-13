package handler

import (
	"errors"
	"net/http"

	"platform-service/internal/biz"
	"platform-service/internal/entity"

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
// POST（注册）开放；/me 需登录；列表与角色/状态修改仅管理员；查询需本人或管理员。
func (h *UserHandler) RegisterRoutes(router *gin.Engine, auth gin.HandlerFunc) {
	group := router.Group("/api/users")
	group.POST("", h.CreateUser)
	group.GET("/me", auth, h.GetMe)
	group.GET("", auth, RequireAdmin(), h.ListUsers)
	group.GET("/:id", auth, h.GetUser)
	group.PATCH("/:id/role", auth, RequireAdmin(), h.SetUserRole)
	group.PATCH("/:id/status", auth, RequireAdmin(), h.SetUserStatus)
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

// GetMe 返回当前登录用户信息
func (h *UserHandler) GetMe(c *gin.Context) {
	user, err := h.userUseCase.GetUser(c.Request.Context(), CurrentUserID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	user.PasswordHash = ""
	c.JSON(http.StatusOK, user)
}

// GetUser 按 id 查询用户（本人或管理员；管理员也可查询）
func (h *UserHandler) GetUser(c *gin.Context) {
	id := c.Param("id")
	if !isAdmin(c) && id != CurrentUserID(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "cannot view other users"})
		return
	}

	user, err := h.userUseCase.GetUser(c.Request.Context(), id)
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

type updateUserRoleRequest struct {
	Role entity.UserRole `json:"role"`
}

// SetUserRole 修改用户角色（仅管理员；不能改自己的角色，防止误锁）
func (h *UserHandler) SetUserRole(c *gin.Context) {
	id := c.Param("id")
	if id == CurrentUserID(c) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot change your own role"})
		return
	}

	var req updateUserRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}
	if req.Role != entity.RoleUser && req.Role != entity.RoleAdmin {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid role"})
		return
	}

	user, err := h.userUseCase.SetRole(c.Request.Context(), id, req.Role)
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

type updateUserStatusRequest struct {
	Status entity.UserStatus `json:"status"`
}

// SetUserStatus 启用/禁用用户（仅管理员；不能禁用自己）
func (h *UserHandler) SetUserStatus(c *gin.Context) {
	id := c.Param("id")
	if id == CurrentUserID(c) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot change your own status"})
		return
	}

	var req updateUserStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}
	if req.Status != entity.UserStatusActive && req.Status != entity.UserStatusDisabled {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid status"})
		return
	}

	user, err := h.userUseCase.SetStatus(c.Request.Context(), id, req.Status)
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
