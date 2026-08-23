package handler

import (
	"errors"
	"net/http"

	"platform-service/internal/biz"
	"platform-service/internal/entity"

	"github.com/gin-gonic/gin"
)

// AdminPlanHandler 管理员套餐/订阅管理（M9）
type AdminPlanHandler struct {
	planUC *biz.PlanUseCase
	subUC  *biz.SubscriptionUseCase
}

func NewAdminPlanHandler(planUC *biz.PlanUseCase, subUC *biz.SubscriptionUseCase) *AdminPlanHandler {
	return &AdminPlanHandler{planUC: planUC, subUC: subUC}
}

func (h *AdminPlanHandler) RegisterRoutes(router *gin.Engine, auth gin.HandlerFunc) {
	group := router.Group("/api/admin")
	group.Use(auth, RequireAdmin())

	group.GET("/plans", h.ListPlans)
	group.POST("/plans", h.CreatePlan)
	group.GET("/plans/:id", h.GetPlan)
	group.PUT("/plans/:id", h.UpdatePlan)
	group.DELETE("/plans/:id", h.DeletePlan)

	group.GET("/subscriptions", h.ListSubscriptions)
	group.POST("/subscriptions/:id/suspend", h.SuspendSubscription)
	group.POST("/subscriptions/:id/unsuspend", h.UnsuspendSubscription)
	group.POST("/subscriptions/:id/cancel", h.CancelSubscription)
}

// planBadRequest 校验类错误 → 400；其余 → 500
func planBadRequest(c *gin.Context, err error) {
	c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
}

type planRequest struct {
	DisplayName         string                  `json:"display_name"`
	Description         string                  `json:"description"`
	PriceCents          int64                   `json:"price_cents"`
	DurationHours       int                     `json:"duration_hours"`
	ResourceCPUMilli    int64                   `json:"resource_cpu_milli"`
	ResourceMemoryBytes int64                   `json:"resource_memory_bytes"`
	ResourceDiskBytes   int64                   `json:"resource_disk_bytes"`
	MaxInstances        int                     `json:"max_instances"` // 订阅内实例数量上限（0 = 不限）
	Basket              []entity.PlanBasketItem `json:"basket"`
	Enabled             *bool                   `json:"enabled"`
}

func (r *planRequest) toEntity() *entity.ServerPlan {
	return &entity.ServerPlan{
		DisplayName:         r.DisplayName,
		Description:         r.Description,
		PriceCents:          r.PriceCents,
		DurationHours:       r.DurationHours,
		ResourceCPUMilli:    r.ResourceCPUMilli,
		ResourceMemoryBytes: r.ResourceMemoryBytes,
		ResourceDiskBytes:   r.ResourceDiskBytes,
		MaxInstances:        r.MaxInstances,
		Basket:              r.Basket,
	}
}

// ListPlans 套餐列表（admin 见全部，含下架）
func (h *AdminPlanHandler) ListPlans(c *gin.Context) {
	plans, err := h.planUC.ListPlans(c.Request.Context(), true)
	if err != nil {
		planBadRequest(c, err); return
	}
	c.JSON(http.StatusOK, gin.H{"plans": plans})
}

// CreatePlan 创建套餐
func (h *AdminPlanHandler) CreatePlan(c *gin.Context) {
	var req planRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()}); return
	}
	plan, err := h.planUC.CreatePlan(c.Request.Context(), req.toEntity())
	if err != nil {
		planBadRequest(c, err); return
	}
	c.JSON(http.StatusCreated, plan)
}

// GetPlan 套餐详情
func (h *AdminPlanHandler) GetPlan(c *gin.Context) {
	plan, err := h.planUC.GetPlan(c.Request.Context(), c.Param("id"))
	if err != nil {
		if errors.Is(err, biz.ErrPlanNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "plan not found"}); return
		}
		planBadRequest(c, err); return
	}
	c.JSON(http.StatusOK, plan)
}

// UpdatePlan 编辑套餐（编辑只影响新购，已购订阅快照不受影响）
func (h *AdminPlanHandler) UpdatePlan(c *gin.Context) {
	var req planRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()}); return
	}
	plan, err := h.planUC.UpdatePlan(c.Request.Context(), c.Param("id"), req.toEntity(), req.Enabled)
	if err != nil {
		if errors.Is(err, biz.ErrPlanNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "plan not found"}); return
		}
		planBadRequest(c, err); return
	}
	c.JSON(http.StatusOK, plan)
}

// DeletePlan 删除套餐（未被引用物理删除；已被订阅引用 → 下架）
func (h *AdminPlanHandler) DeletePlan(c *gin.Context) {
	if err := h.planUC.DeletePlan(c.Request.Context(), c.Param("id")); err != nil {
		planBadRequest(c, err); return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

// ListSubscriptions 订阅列表（admin 全量）
func (h *AdminPlanHandler) ListSubscriptions(c *gin.Context) {
	subs, err := h.subUC.List(c.Request.Context(), "")
	if err != nil {
		planBadRequest(c, err); return
	}
	c.JSON(http.StatusOK, gin.H{"subscriptions": subs})
}

// SuspendSubscription 管理员停用订阅（停止活跃实例 + 禁 start）
func (h *AdminPlanHandler) SuspendSubscription(c *gin.Context) {
	sub, err := h.subUC.Suspend(c.Request.Context(), c.Param("id"))
	if err != nil {
		if errors.Is(err, biz.ErrSubscriptionNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "subscription not found"}); return
		}
		planBadRequest(c, err); return
	}
	c.JSON(http.StatusOK, sub)
}

// UnsuspendSubscription 管理员恢复停用订阅
func (h *AdminPlanHandler) UnsuspendSubscription(c *gin.Context) {
	sub, err := h.subUC.Unsuspend(c.Request.Context(), c.Param("id"))
	if err != nil {
		if errors.Is(err, biz.ErrSubscriptionNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "subscription not found"}); return
		}
		planBadRequest(c, err); return
	}
	c.JSON(http.StatusOK, sub)
}

// CancelSubscription 管理员取消订阅（停止活跃实例 + 禁 start）
func (h *AdminPlanHandler) CancelSubscription(c *gin.Context) {
	sub, err := h.subUC.Cancel(c.Request.Context(), "", c.Param("id"))
	if err != nil {
		if errors.Is(err, biz.ErrSubscriptionNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "subscription not found"}); return
		}
		planBadRequest(c, err); return
	}
	c.JSON(http.StatusOK, sub)
}
