package handler

import (
	"errors"
	"net/http"

	"platform-service/internal/biz"
	"platform-service/internal/client/controller"
	"platform-service/internal/entity"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// OrderHandler 提供订单相关的 HTTP 接口
type OrderHandler struct {
	orderUseCase *biz.OrderUseCase
}

func NewOrderHandler(uc *biz.OrderUseCase) *OrderHandler {
	return &OrderHandler{orderUseCase: uc}
}

// RegisterRoutes 注册订单路由（全部需登录）
func (h *OrderHandler) RegisterRoutes(router *gin.Engine, auth gin.HandlerFunc) {
	group := router.Group("/api/orders")
	group.Use(auth)
	group.POST("", h.CreateOrder)
	group.GET("", h.ListOrders)
	group.GET("/:id", h.GetOrder)
	group.POST("/:id/pay", h.PayOrder)
	group.POST("/:id/provision", auth, RequireAdmin(), h.ProvisionOrder)
	group.POST("/:id/instance/start", h.StartInstance)
	group.POST("/:id/instance/stop", h.StopInstance)

	// 用户侧实例视图 + 文件会话
	router.GET("/api/me/instances", auth, h.MyInstances)
	router.POST("/api/me/instances/:orderId/file-session", auth, h.MyFileSession)
	router.PUT("/api/me/instances/:orderId/config", auth, h.MyInstanceConfig)
	router.GET("/api/instances", auth, RequireAdmin(), h.AllInstances)

	// 游戏配置 schema（M5）：下单表单数据源（透传 controller → asset_service）
	router.GET("/api/games/:id/config-schema", auth, h.ConfigSchema)
}

type createOrderRequest struct {
	UserID string            `json:"user_id"` // 仅管理员可指定；普通用户忽略，取自 token
	GameID string            `json:"game_id"`
	Amount int64             `json:"amount"` // 单位：分
	Config map[string]string `json:"config,omitempty"` // 实例配置（游戏配置 schema 声明的键值）
}

// CreateOrder 创建订单（user_id 强制取自 token，普通用户不可伪造）
func (h *OrderHandler) CreateOrder(c *gin.Context) {
	var req createOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}

	userID := CurrentUserID(c)
	if isAdmin(c) && req.UserID != "" {
		userID = req.UserID
	}

	order, err := h.orderUseCase.CreateOrder(c.Request.Context(), userID, req.GameID, req.Amount, req.Config)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, order)
}

// ConfigSchema 游戏配置 schema（下单表单数据源，透传 controller → asset_service）
func (h *OrderHandler) ConfigSchema(c *gin.Context) {
	schema, err := h.orderUseCase.ConfigSchema(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, schema)
}

// ListOrders 列出订单：普通用户只看自己的；管理员看全部（可用 ?user_id=、?game_id= 过滤）
func (h *OrderHandler) ListOrders(c *gin.Context) {
	userID := ""
	if isAdmin(c) {
		userID = c.Query("user_id")
	} else {
		userID = CurrentUserID(c)
	}

	orders, err := h.orderUseCase.ListOrders(c.Request.Context(), userID, c.Query("game_id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"orders": orders})
}

// GetOrder 查询订单（本人或管理员）
func (h *OrderHandler) GetOrder(c *gin.Context) {
	id := c.Param("id")
	order, err := h.orderUseCase.GetOrder(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !isAdmin(c) && order.UserID != CurrentUserID(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "cannot view other users orders"})
		return
	}
	c.JSON(http.StatusOK, order)
}

// PayOrder 支付订单（本人或管理员）并编排实例：controller 创建 + 启动
func (h *OrderHandler) PayOrder(c *gin.Context) {
	id := c.Param("id")
	order, err := h.orderUseCase.GetOrder(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !isAdmin(c) && order.UserID != CurrentUserID(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "cannot pay other users orders"})
		return
	}

	updated, err := h.orderUseCase.PayOrder(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, updated)
}

// StartInstance 启动订单关联实例（本人或管理员）
func (h *OrderHandler) StartInstance(c *gin.Context) {
	order, ok := h.loadOwnOrder(c)
	if !ok {
		return
	}
	if _, err := h.orderUseCase.StartInstance(c.Request.Context(), order.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "starting"})
}

// StopInstance 停止订单关联实例（本人或管理员）
func (h *OrderHandler) StopInstance(c *gin.Context) {
	order, ok := h.loadOwnOrder(c)
	if !ok {
		return
	}
	if _, err := h.orderUseCase.StopInstance(c.Request.Context(), order.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "stopping"})
}

// loadOwnOrder 加载订单并校验归属（本人或管理员），校验失败时已写响应并返回 ok=false
func (h *OrderHandler) loadOwnOrder(c *gin.Context) (*entity.Order, bool) {
	order, err := h.orderUseCase.GetOrder(c.Request.Context(), c.Param("id"))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
			return nil, false
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return nil, false
	}
	if !isAdmin(c) && order.UserID != CurrentUserID(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "cannot operate other users orders"})
		return nil, false
	}
	return order, true
}

// ProvisionOrder 管理员免支付直接开服（ADR：支付与开服解耦）
func (h *OrderHandler) ProvisionOrder(c *gin.Context) {
	id := c.Param("id")
	updated, err := h.orderUseCase.ProvisionOrder(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, updated)
}

// MyInstances 当前用户的实例列表（?game_id= 过滤）
func (h *OrderHandler) MyInstances(c *gin.Context) {
	instances, err := h.orderUseCase.ListInstances(c.Request.Context(), CurrentUserID(c), c.Query("game_id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"instances": instances})
}

// MyFileSession 为当前用户订单关联的实例签发文件会话（本人或管理员）。
// 注意：路由参数是 :orderId，不能复用 loadOwnOrder（其内部取 c.Param("id")）。func (h *OrderHandler) MyFileSession(c *gin.Context) {
	orderID := c.Param("orderId")
	order, err := h.orderUseCase.GetOrder(c.Request.Context(), orderID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !isAdmin(c) && order.UserID != CurrentUserID(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "cannot access other users instances"})
		return
	}
	if order.InstanceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "order has no instance, pay or provision first"})
		return
	}
	session, err := h.orderUseCase.FileSession(c.Request.Context(), order.InstanceID)
	if err != nil {
		if errors.Is(err, controller.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		// controller 不可达或返回错误：502 + 透传 controller 的响应体便于排查
		c.JSON(http.StatusBadGateway, gin.H{"error": "controller 不可达或返回错误: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, session)
}

type updateInstanceConfigRequest struct {
	Config map[string]string `json:"config"`
}

// MyInstanceConfig 更新当前用户订单关联实例的配置（本人或管理员；重启生效）。
func (h *OrderHandler) MyInstanceConfig(c *gin.Context) {
	orderID := c.Param("orderId")
	order, err := h.orderUseCase.GetOrder(c.Request.Context(), orderID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !isAdmin(c) && order.UserID != CurrentUserID(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "cannot access other users instances"})
		return
	}
	var req updateInstanceConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}
	if err := h.orderUseCase.UpdateInstanceConfig(c.Request.Context(), orderID, req.Config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "config updated (takes effect on next start)"})
}

// AllInstances 全部实例（管理员；?game_id= 过滤）
func (h *OrderHandler) AllInstances(c *gin.Context) {
	instances, err := h.orderUseCase.ListInstances(c.Request.Context(), "", c.Query("game_id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"instances": instances})
}
