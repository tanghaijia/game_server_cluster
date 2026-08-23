package biz

import (
	"context"
	"errors"
	"fmt"
	"time"

	"platform-service/internal/entity"
	"platform-service/internal/repository"

	"gorm.io/gorm"
)

// 套餐领域错误（handler 据此映射 HTTP 状态码）
var ErrPlanNotFound = errors.New("plan not found")

// PlanUseCase 套餐（SKU）业务逻辑：管理员增删设置，编辑只影响新购订阅。
type PlanUseCase struct {
	planRepo repository.ServerPlanRepository
	subRepo  repository.SubscriptionRepository
}

func NewPlanUseCase(planRepo repository.ServerPlanRepository, subRepo repository.SubscriptionRepository) *PlanUseCase {
	return &PlanUseCase{planRepo: planRepo, subRepo: subRepo}
}

// validatePlan 校验套餐字段：名称必填、金额/时长非负、篮子至少一项且 game_id 唯一。
func validatePlan(p *entity.ServerPlan) error {
	if p == nil {
		return errors.New("plan is required")
	}
	if p.DisplayName == "" {
		return errors.New("display_name is required")
	}
	if p.PriceCents < 0 {
		return errors.New("price_cents must be non-negative")
	}
	if p.DurationHours < 0 {
		return errors.New("duration_hours must be non-negative")
	}
	if p.MaxInstances < 0 {
		return errors.New("max_instances must be non-negative")
	}
	if len(p.Basket) == 0 {
		return errors.New("basket must contain at least one game")
	}
	seen := make(map[string]bool, len(p.Basket))
	for i, item := range p.Basket {
		if item.GameID == "" {
			return fmt.Errorf("basket[%d].game_id is required", i)
		}
		if seen[item.GameID] {
			return fmt.Errorf("basket contains duplicate game_id %q", item.GameID)
		}
		seen[item.GameID] = true
	}
	return nil
}

// CreatePlan 创建套餐（admin）
func (uc *PlanUseCase) CreatePlan(ctx context.Context, plan *entity.ServerPlan) (*entity.ServerPlan, error) {
	if err := validatePlan(plan); err != nil {
		return nil, err
	}
	now := time.Now()
	plan.ID = newEntityID("plan")
	plan.Enabled = true
	plan.CreateTime = now
	plan.UpdateTime = now
	if err := uc.planRepo.Save(ctx, plan); err != nil {
		return nil, err
	}
	return plan, nil
}

// UpdatePlan 编辑套餐（admin）：整字段替换语义（价格/时长可为 0 等合法值），
// 篮子整体替换；enabled 单独用指针（nil = 不修改）。
// 快照语义：已购订阅的 basket_snapshot 不受影响（购买时已固化）。
func (uc *PlanUseCase) UpdatePlan(ctx context.Context, id string, updates *entity.ServerPlan, enabled *bool) (*entity.ServerPlan, error) {
	if id == "" {
		return nil, errors.New("plan id is required")
	}
	plan, err := uc.planRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPlanNotFound
		}
		return nil, err
	}
	if updates.DisplayName != "" {
		plan.DisplayName = updates.DisplayName
	}
	plan.Description = updates.Description
	plan.PriceCents = updates.PriceCents
	plan.DurationHours = updates.DurationHours
	plan.ResourceCPUMilli = updates.ResourceCPUMilli
	plan.ResourceMemoryBytes = updates.ResourceMemoryBytes
	plan.ResourceDiskBytes = updates.ResourceDiskBytes
	plan.MaxInstances = updates.MaxInstances
	if updates.Basket != nil {
		plan.Basket = updates.Basket
	}
	if enabled != nil {
		plan.Enabled = *enabled
	}
	if err := validatePlan(plan); err != nil {
		return nil, err
	}
	plan.UpdateTime = time.Now()
	if err := uc.planRepo.Save(ctx, plan); err != nil {
		return nil, err
	}
	return plan, nil
}

// DeletePlan 删除/下架套餐（admin）：
// 未被任何订阅引用 → 物理删除；已被引用 → 下架（enabled=false），已购订阅不受影响。
func (uc *PlanUseCase) DeletePlan(ctx context.Context, id string) error {
	if id == "" {
		return errors.New("plan id is required")
	}
	subs, err := uc.subRepo.ListByPlan(ctx, id)
	if err != nil {
		return err
	}
	if len(subs) > 0 {
		plan, err := uc.planRepo.GetByID(ctx, id)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrPlanNotFound
			}
			return err
		}
		plan.Enabled = false
		plan.UpdateTime = time.Now()
		return uc.planRepo.Save(ctx, plan)
	}
	return uc.planRepo.Delete(ctx, id)
}

// ListPlans 套餐列表。includeDisabled=true（admin）返回全部；否则仅 enabled（用户侧购买入口）。
func (uc *PlanUseCase) ListPlans(ctx context.Context, includeDisabled bool) ([]*entity.ServerPlan, error) {
	if includeDisabled {
		return uc.planRepo.ListAll(ctx)
	}
	return uc.planRepo.ListEnabled(ctx)
}

// GetPlan 单个套餐
func (uc *PlanUseCase) GetPlan(ctx context.Context, id string) (*entity.ServerPlan, error) {
	if id == "" {
		return nil, errors.New("plan id is required")
	}
	plan, err := uc.planRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPlanNotFound
		}
		return nil, err
	}
	return plan, nil
}
