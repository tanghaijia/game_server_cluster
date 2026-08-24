package biz

import (
	"context"
	"errors"
	"fmt"
	"time"

	"platform-service/internal/client/controller"
	"platform-service/internal/entity"
	"platform-service/internal/repository"

	"gorm.io/gorm"
)

// 订阅领域错误（handler 据此映射 HTTP 状态码）
var ErrSubscriptionNotFound = errors.New("subscription not found")

// SubscriptionController 订阅用到的 controller 客户端能力（M11 实例操作；接口便于测试替换）
type SubscriptionController interface {
	CreateGameInstance(ctx context.Context, gameID, buildID, subscriptionID string, config map[string]string) (*controller.GameInstance, error)
	GetGameInstance(ctx context.Context, instanceID string) (*controller.GameInstance, error)
	StartGameInstance(ctx context.Context, instanceID string) error
	StopGameInstance(ctx context.Context, instanceID string) error
	ListGameInstancesBySubscription(ctx context.Context, subscriptionID string) ([]controller.GameInstance, error)
	// B-04/P1-1：实例运行时统计（健康 + 在线人数）
	GetInstanceRuntime(ctx context.Context, instanceID string) (*controller.InstanceRuntime, error)
}

// SubscriptionUseCase 订阅业务逻辑：用户购买套餐获得订阅，订阅内可创建多个游戏实例
// （M11：创建/启动/停止/列表；单活跃约束在 controller 落地，409 透传）。
type SubscriptionUseCase struct {
	subRepo    repository.SubscriptionRepository
	planUC     *PlanUseCase
	controller SubscriptionController
}

func NewSubscriptionUseCase(subRepo repository.SubscriptionRepository, planUC *PlanUseCase, controllerClient SubscriptionController) *SubscriptionUseCase {
	return &SubscriptionUseCase{subRepo: subRepo, planUC: planUC, controller: controllerClient}
}

// Purchase 购买套餐 → 创建订阅（占位：无真实支付渠道，直接激活）。
// 购买时把套餐篮子快照进 basket_snapshot（快照语义，套餐后续编辑不追溯）。
// duration_hours > 0 → expires_at = now + duration；0 = 不过期。
func (uc *SubscriptionUseCase) Purchase(ctx context.Context, userID, planID string) (*entity.Subscription, error) {
	if userID == "" {
		return nil, errors.New("user_id is required")
	}
	plan, err := uc.planUC.GetPlan(ctx, planID)
	if err != nil {
		if errors.Is(err, ErrPlanNotFound) {
			return nil, ErrPlanNotFound
		}
		return nil, err
	}
	if !plan.Enabled {
		return nil, errors.New("plan is disabled")
	}

	now := time.Now()
	sub := &entity.Subscription{
		ID:             newEntityID("sub"),
		UserID:         userID,
		PlanID:         plan.ID,
		Status:         entity.SubscriptionActive,
		BasketSnapshot: plan.Basket,
		MaxInstances:   plan.MaxInstances, // 实例数量上限快照（0 = 不限）
		CreateTime:     now,
		UpdateTime:     now,
	}
	if plan.DurationHours > 0 {
		exp := now.Add(time.Duration(plan.DurationHours) * time.Hour)
		sub.ExpiresAt = &exp
	}
	if err := uc.subRepo.Save(ctx, sub); err != nil {
		return nil, err
	}
	return sub, nil
}

// List 订阅列表：userID 非空只列该用户（用户视角）；空表示全部（admin）。
func (uc *SubscriptionUseCase) List(ctx context.Context, userID string) ([]*entity.Subscription, error) {
	if userID != "" {
		return uc.subRepo.ListByUser(ctx, userID)
	}
	return uc.subRepo.ListAll(ctx)
}

// ListEnabledPlans 在售套餐列表（用户购买入口，M12）
func (uc *SubscriptionUseCase) ListEnabledPlans(ctx context.Context) ([]*entity.ServerPlan, error) {
	return uc.planUC.ListPlans(ctx, false)
}

// Get 单个订阅
func (uc *SubscriptionUseCase) Get(ctx context.Context, id string) (*entity.Subscription, error) {
	if id == "" {
		return nil, errors.New("subscription id is required")
	}
	sub, err := uc.subRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSubscriptionNotFound
		}
		return nil, err
	}
	return sub, nil
}

// Suspend 管理员停用订阅：状态迁移 + 停止其活跃实例（M12 扩展）。
func (uc *SubscriptionUseCase) Suspend(ctx context.Context, id string) (*entity.Subscription, error) {
	sub, err := uc.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if sub.Status != entity.SubscriptionActive {
		return nil, errors.New("only active subscription can be suspended")
	}
	if err := uc.stopActiveInstances(ctx, sub); err != nil {
		return nil, fmt.Errorf("stop active instances: %w", err)
	}
	sub.Status = entity.SubscriptionSuspended
	sub.UpdateTime = time.Now()
	if err := uc.subRepo.Save(ctx, sub); err != nil {
		return nil, err
	}
	return sub, nil
}

// Unsuspend 管理员恢复停用订阅：suspended → active（续期由 Renew 负责）。
func (uc *SubscriptionUseCase) Unsuspend(ctx context.Context, id string) (*entity.Subscription, error) {
	sub, err := uc.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if sub.Status != entity.SubscriptionSuspended {
		return nil, errors.New("only suspended subscription can be unsuspended")
	}
	sub.Status = entity.SubscriptionActive
	sub.UpdateTime = time.Now()
	if err := uc.subRepo.Save(ctx, sub); err != nil {
		return nil, err
	}
	return sub, nil
}

// Cancel 取消订阅（用户取消自己的，或 admin 取消任意）：状态迁移 + 停止活跃实例。
func (uc *SubscriptionUseCase) Cancel(ctx context.Context, userID, id string) (*entity.Subscription, error) {
	sub, err := uc.ownSubscription(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if sub.Status != entity.SubscriptionActive && sub.Status != entity.SubscriptionSuspended {
		return nil, errors.New("only active/suspended subscription can be cancelled")
	}
	if err := uc.stopActiveInstances(ctx, sub); err != nil {
		return nil, fmt.Errorf("stop active instances: %w", err)
	}
	sub.Status = entity.SubscriptionCancelled
	sub.UpdateTime = time.Now()
	if err := uc.subRepo.Save(ctx, sub); err != nil {
		return nil, err
	}
	return sub, nil
}

// Renew 续费订阅（用户操作）：active/expired → active，
// expires_at 从 max(now, 当前到期) 起延长 plan.duration_hours（0 = 转为永久）。
func (uc *SubscriptionUseCase) Renew(ctx context.Context, userID, id string) (*entity.Subscription, error) {
	sub, err := uc.ownSubscription(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if sub.Status != entity.SubscriptionActive && sub.Status != entity.SubscriptionExpired {
		return nil, errors.New("only active/expired subscription can be renewed")
	}
	plan, err := uc.planUC.GetPlan(ctx, sub.PlanID)
	if err != nil {
		return nil, fmt.Errorf("load plan: %w", err)
	}
	if plan.DurationHours <= 0 {
		return nil, errors.New("plan has no duration, cannot renew")
	}
	base := time.Now()
	if sub.ExpiresAt != nil && sub.ExpiresAt.After(base) {
		base = *sub.ExpiresAt // 未到期续费：从原到期时间累加
	}
	exp := base.Add(time.Duration(plan.DurationHours) * time.Hour)
	sub.ExpiresAt = &exp
	sub.Status = entity.SubscriptionActive
	sub.UpdateTime = time.Now()
	if err := uc.subRepo.Save(ctx, sub); err != nil {
		return nil, err
	}
	return sub, nil
}

// ExpireOverdue 到期 sweep（M12，platform 后台定时调用）：
// 扫出已到期订阅 → 停止其活跃实例 → 标记 expired。返回本次处理数量。
func (uc *SubscriptionUseCase) ExpireOverdue(ctx context.Context) (int, error) {
	overdue, err := uc.subRepo.ListOverdue(ctx, time.Now())
	if err != nil {
		return 0, err
	}
	expired := 0
	for _, sub := range overdue {
		if err := uc.stopActiveInstances(ctx, sub); err != nil {
			return expired, fmt.Errorf("subscription %s: %w", sub.ID, err)
		}
		sub.Status = entity.SubscriptionExpired
		sub.UpdateTime = time.Now()
		if err := uc.subRepo.Save(ctx, sub); err != nil {
			return expired, fmt.Errorf("mark expired %s: %w", sub.ID, err)
		}
		expired++
	}
	return expired, nil
}

// stopActiveInstances 停止订阅下所有活跃实例（running 等；controller 仅允许 running/failed
// 状态停止，中间态实例由下一轮 sweep 收敛，≤1 个周期窗口）。
func (uc *SubscriptionUseCase) stopActiveInstances(ctx context.Context, sub *entity.Subscription) error {
	insts, err := uc.controller.ListGameInstancesBySubscription(ctx, sub.ID)
	if err != nil {
		return err
	}
	var firstErr error
	for _, inst := range insts {
		if inst.Status == "stopped" || inst.Status == "failed" {
			continue
		}
		if err := uc.controller.StopGameInstance(ctx, inst.ID); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// ------------------------- M11：订阅内实例 -------------------------

// presetForGame 从订阅篮子快照找某游戏的 preset（含默认配置）
func presetForGame(sub *entity.Subscription, gameID string) *entity.PlanBasketItem {
	for i := range sub.BasketSnapshot {
		if sub.BasketSnapshot[i].GameID == gameID {
			return &sub.BasketSnapshot[i]
		}
	}
	return nil
}

// checkActive 校验订阅可用：status=active 且未到期
func checkActive(sub *entity.Subscription) error {
	if sub.Status != entity.SubscriptionActive {
		return fmt.Errorf("subscription is %s（仅 active 可用）", sub.Status)
	}
	if sub.ExpiresAt != nil && sub.ExpiresAt.Before(time.Now()) {
		return errors.New("subscription has expired")
	}
	return nil
}

// ownSubscription 校验订阅归属（用户只能操作自己的订阅）
func (uc *SubscriptionUseCase) ownSubscription(ctx context.Context, userID, subscriptionID string) (*entity.Subscription, error) {
	sub, err := uc.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	if userID != "" && sub.UserID != userID {
		return nil, errors.New("subscription does not belong to this user")
	}
	return sub, nil
}

// verifyInstanceOwnership 校验：订阅归属 + 订阅可用 + 实例确实属于该订阅 + 实例游戏在篮子内。
// 返回订阅实体（供调用方继续使用）。
func (uc *SubscriptionUseCase) verifyInstanceOwnership(ctx context.Context, userID, subscriptionID, instanceID string) (*entity.Subscription, error) {
	sub, err := uc.ownSubscription(ctx, userID, subscriptionID)
	if err != nil {
		return nil, err
	}
	if err := checkActive(sub); err != nil {
		return nil, err
	}
	inst, err := uc.controller.GetGameInstance(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("load instance: %w", err)
	}
	if inst.SubscriptionID == nil || *inst.SubscriptionID != subscriptionID {
		return nil, errors.New("instance does not belong to this subscription")
	}
	if presetForGame(sub, inst.GameID) == nil {
		return nil, fmt.Errorf("instance game %s is not in subscription basket", inst.GameID)
	}
	return sub, nil
}

// CreateInstance 订阅内创建实例（M11）：初始 stopped，不占单活跃槽位。
// 校验归属/激活/未到期、game ∈ 篮子快照；preset 默认配置与请求配置合并（请求覆盖 preset）。
// controller 做 schema 校验，故 preset 中非法 key 会在创建时报错（安全）。
func (uc *SubscriptionUseCase) CreateInstance(ctx context.Context, userID, subscriptionID, gameID string, config map[string]string) (*controller.GameInstance, error) {
	sub, err := uc.ownSubscription(ctx, userID, subscriptionID)
	if err != nil {
		return nil, err
	}
	if err := checkActive(sub); err != nil {
		return nil, err
	}
	if gameID == "" {
		return nil, errors.New("game_id is required")
	}
	preset := presetForGame(sub, gameID)
	if preset == nil {
		return nil, fmt.Errorf("game %s is not in subscription basket", gameID)
	}
	// M13：实例数量上限（购买时快照，0 = 不限）
	if sub.MaxInstances > 0 {
		insts, err := uc.controller.ListGameInstancesBySubscription(ctx, sub.ID)
		if err != nil {
			return nil, fmt.Errorf("list subscription instances: %w", err)
		}
		if len(insts) >= sub.MaxInstances {
			return nil, fmt.Errorf("订阅实例数已达上限 %d，请先删除不再使用的实例", sub.MaxInstances)
		}
	}
	merged := map[string]string{}
	for k, v := range preset.Config {
		merged[k] = v
	}
	for k, v := range config {
		merged[k] = v
	}
	return uc.controller.CreateGameInstance(ctx, gameID, "", subscriptionID, merged)
}

// StartInstance 订阅内启动实例（M11）。controller 校验单活跃约束，冲突返回 ErrConflict。
func (uc *SubscriptionUseCase) StartInstance(ctx context.Context, userID, subscriptionID, instanceID string) error {
	if _, err := uc.verifyInstanceOwnership(ctx, userID, subscriptionID, instanceID); err != nil {
		return err
	}
	return uc.controller.StartGameInstance(ctx, instanceID)
}

// StopInstance 订阅内停止实例。允许过期/停用订阅下的停止（清理场景不设 checkActive）。
func (uc *SubscriptionUseCase) StopInstance(ctx context.Context, userID, subscriptionID, instanceID string) error {
	if _, err := uc.ownSubscription(ctx, userID, subscriptionID); err != nil {
		return err
	}
	inst, err := uc.controller.GetGameInstance(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("load instance: %w", err)
	}
	if inst.SubscriptionID == nil || *inst.SubscriptionID != subscriptionID {
		return errors.New("instance does not belong to this subscription")
	}
	return uc.controller.StopGameInstance(ctx, instanceID)
}

// ListInstances 订阅内实例列表（M11）
func (uc *SubscriptionUseCase) ListInstances(ctx context.Context, userID, subscriptionID string) ([]controller.GameInstance, error) {
	if _, err := uc.ownSubscription(ctx, userID, subscriptionID); err != nil {
		return nil, err
	}
	return uc.controller.ListGameInstancesBySubscription(ctx, subscriptionID)
}

// GetInstanceRuntime 订阅内实例运行时统计（B-04/P1-1：在线人数 + 健康）。
// 只做归属校验（订阅归属 + 实例归属订阅），不做 checkActive —— 只读遥测，过期订阅也允许查看。
func (uc *SubscriptionUseCase) GetInstanceRuntime(ctx context.Context, userID, subscriptionID, instanceID string) (*controller.InstanceRuntime, error) {
	if _, err := uc.ownSubscription(ctx, userID, subscriptionID); err != nil {
		return nil, err
	}
	inst, err := uc.controller.GetGameInstance(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("load instance: %w", err)
	}
	if inst.SubscriptionID == nil || *inst.SubscriptionID != subscriptionID {
		return nil, errors.New("instance does not belong to this subscription")
	}
	return uc.controller.GetInstanceRuntime(ctx, instanceID)
}
