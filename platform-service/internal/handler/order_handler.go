package handler

import (
	"errors"
	"net/http"

	"platform-service/internal/biz"

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

// RegisterRoutes 注册订单路由（全部需要登录）
func (h *OrderHandler) RegisterRoutes(router *gin.Engine, auth gin.HandlerFunc) {
	group := router.Group("/api/orders")
	group.Use(auth)
	group.POST("", h.CreateOrder)
	group.GET("", h.ListOrders)
	group.GET("/:id", h.GetOrder)
}

type createOrderRequest struct {
	UserID string `json:"user_id"`
	GameID string `json:"game_id"`
	Amount int64  `json:"amount"` // 单位：分
}

// CreateOrder 创建订单
func (h *OrderHandler) CreateOrder(c *gin.Context) {
	var req createOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}

	order, err := h.orderUseCase.CreateOrder(c.Request.Context(), req.UserID, req.GameID, req.Amount)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, order)
}

// ListOrders 列出订单；?user_id= 过滤指定用户
func (h *OrderHandler) ListOrders(c *gin.Context) {
	orders, err := h.orderUseCase.ListOrders(c.Request.Context(), c.Query("user_id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"orders": orders})
}

// GetOrder 按 id 查询订单
func (h *OrderHandler) GetOrder(c *gin.Context) {
	order, err := h.orderUseCase.GetOrder(c.Request.Context(), c.Param("id"))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, order)
}
