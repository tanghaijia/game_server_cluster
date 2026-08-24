package handler

import (
	"errors"
	"net/http"

	"platform-service/internal/biz"
	"platform-service/internal/client/controller"

	"github.com/gin-gonic/gin"
)

// SubscriptionHandler 用户订阅（M9）：购买套餐、查看我的订阅
type SubscriptionHandler struct {
	subUC *biz.SubscriptionUseCase
}

func NewSubscriptionHandler(subUC *biz.SubscriptionUseCase) *SubscriptionHandler {
	return &SubscriptionHandler{subUC: subUC}
}

func (h *SubscriptionHandler) RegisterRoutes(router *gin.Engine, auth gin.HandlerFunc) {
	group := router.Group("/api/me/subscriptions", auth)
	group.GET("", h.ListMine)
	group.POST("", h.Purchase)
	group.POST("/:id/renew", h.Renew)
	group.POST("/:id/cancel", h.Cancel)
	// M11：订阅内实例（创建/列表/启停）
	group.GET("/:id/instances", h.ListInstances)
	group.POST("/:id/instances", h.CreateInstance)
	group.POST("/:id/instances/:instanceId/start", h.StartInstance)
	group.POST("/:id/instances/:instanceId/stop", h.StopInstance)
	// B-04/P1-1：实例运行时统计（健康 + 在线人数）
	group.GET("/:id/instances/:instanceId/runtime", h.GetInstanceRuntime)

	// M12：在售套餐（购买入口，非 admin 也可见）
	router.GET("/api/me/plans", auth, h.ListPlans)
}

// ListMine 我的订阅列表
func (h *SubscriptionHandler) ListMine(c *gin.Context) {
	subs, err := h.subUC.List(c.Request.Context(), CurrentUserID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()}); return
	}
	c.JSON(http.StatusOK, gin.H{"subscriptions": subs})
}

type purchaseRequest struct {
	PlanID string `json:"plan_id"`
}

// Purchase 购买套餐 → 创建订阅（占位支付，直接激活）
func (h *SubscriptionHandler) Purchase(c *gin.Context) {
	var req purchaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()}); return
	}
	if req.PlanID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "plan_id is required"}); return
	}
	sub, err := h.subUC.Purchase(c.Request.Context(), CurrentUserID(c), req.PlanID)
	if err != nil {
		if errors.Is(err, biz.ErrPlanNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "plan not found"}); return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()}); return
	}
	c.JSON(http.StatusCreated, sub)
}

// ListPlans 在售套餐列表（用户购买入口）
func (h *SubscriptionHandler) ListPlans(c *gin.Context) {
	plans, err := h.subUC.ListEnabledPlans(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()}); return
	}
	c.JSON(http.StatusOK, gin.H{"plans": plans})
}

// Renew 续费订阅（active/expired → active，expires_at 按套餐时长延长）
func (h *SubscriptionHandler) Renew(c *gin.Context) {
	sub, err := h.subUC.Renew(c.Request.Context(), CurrentUserID(c), c.Param("id"))
	if err != nil {
		subError(c, err); return
	}
	c.JSON(http.StatusOK, sub)
}

// Cancel 取消订阅（用户取消自己的；停止活跃实例）
func (h *SubscriptionHandler) Cancel(c *gin.Context) {
	sub, err := h.subUC.Cancel(c.Request.Context(), CurrentUserID(c), c.Param("id"))
	if err != nil {
		subError(c, err); return
	}
	c.JSON(http.StatusOK, sub)
}

// ------------------------- M11：订阅内实例 -------------------------

// subError 统一错误映射：订阅不存在 404 / controller 冲突 409 / 其余 400
func subError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, biz.ErrSubscriptionNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "subscription not found"})
	case errors.Is(err, controller.ErrConflict):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, controller.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}
}

// ListInstances 订阅内实例列表
func (h *SubscriptionHandler) ListInstances(c *gin.Context) {
	insts, err := h.subUC.ListInstances(c.Request.Context(), CurrentUserID(c), c.Param("id"))
	if err != nil {
		subError(c, err); return
	}
	c.JSON(http.StatusOK, gin.H{"instances": insts})
}

type createSubscriptionInstanceRequest struct {
	GameID string            `json:"game_id"`
	Config map[string]string `json:"config,omitempty"` // 覆盖 preset 的键值（schema 校验由 controller 做）
}

// CreateInstance 订阅内创建实例（初始 stopped，不占单活跃槽位）
func (h *SubscriptionHandler) CreateInstance(c *gin.Context) {
	var req createSubscriptionInstanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()}); return
	}
	inst, err := h.subUC.CreateInstance(c.Request.Context(), CurrentUserID(c), c.Param("id"), req.GameID, req.Config)
	if err != nil {
		subError(c, err); return
	}
	c.JSON(http.StatusCreated, inst)
}

// StartInstance 订阅内启动实例（controller 校验单活跃约束，冲突 409）
func (h *SubscriptionHandler) StartInstance(c *gin.Context) {
	if err := h.subUC.StartInstance(c.Request.Context(), CurrentUserID(c), c.Param("id"), c.Param("instanceId")); err != nil {
		subError(c, err); return
	}
	c.JSON(http.StatusOK, gin.H{"message": "started"})
}

// StopInstance 订阅内停止实例
func (h *SubscriptionHandler) StopInstance(c *gin.Context) {
	if err := h.subUC.StopInstance(c.Request.Context(), CurrentUserID(c), c.Param("id"), c.Param("instanceId")); err != nil {
		subError(c, err); return
	}
	c.JSON(http.StatusOK, gin.H{"message": "stopping"})
}

// GetInstanceRuntime 订阅内实例运行时统计（B-04/P1-1：在线人数 + 健康）
func (h *SubscriptionHandler) GetInstanceRuntime(c *gin.Context) {
	rt, err := h.subUC.GetInstanceRuntime(c.Request.Context(), CurrentUserID(c), c.Param("id"), c.Param("instanceId"))
	if err != nil {
		subError(c, err); return
	}
	c.JSON(http.StatusOK, rt)
}
