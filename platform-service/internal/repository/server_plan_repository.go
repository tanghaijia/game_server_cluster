package repository

import (
	"context"

	"platform-service/internal/entity"
)

// ServerPlanRepository 套餐数据层接口
type ServerPlanRepository interface {
	Save(ctx context.Context, plan *entity.ServerPlan) error
	GetByID(ctx context.Context, id string) (*entity.ServerPlan, error)
	ListAll(ctx context.Context) ([]*entity.ServerPlan, error)
	ListEnabled(ctx context.Context) ([]*entity.ServerPlan, error)
	// Delete 物理删除（仅未被订阅引用的套餐可删；被引用时业务层改为下架 enabled=false）
	Delete(ctx context.Context, id string) error
}
