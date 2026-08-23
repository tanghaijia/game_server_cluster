package repository

import (
	"context"

	"controller-go/internal/entity"
)

// CredentialPoolRepository 外部受限凭证池数据层（M8，见 §3.6.5）
type CredentialPoolRepository interface {
	// ListByGame 列出某游戏某类型的全部凭证（按创建时间倒序）
	ListByGame(ctx context.Context, gameID, resourceType string) ([]entity.CredentialPool, error)
	// GetByID 查询单条
	GetByID(ctx context.Context, id string) (*entity.CredentialPool, error)
	// Create 批量录入（每条一条记录）
	Create(ctx context.Context, creds []entity.CredentialPool) error
	// Delete 删除（in_use 记录由调用方先校验）
	Delete(ctx context.Context, id string) error
	// Acquire 原子分配：仅 available 记录可被抢占（返回是否成功）
	Acquire(ctx context.Context, id, instanceID string) (bool, error)
	// Release 原子释放：仅该实例占用的 in_use 记录可归还（幂等）
	Release(ctx context.Context, id, instanceID string) (bool, error)
	// ReleaseByInstance 释放某实例占用的全部凭证（幂等；用于失败/停止路径）
	ReleaseByInstance(ctx context.Context, instanceID string) error
	// FindAllocatedByInstance 查询某实例当前占用的凭证（幂等分配用）
	FindAllocatedByInstance(ctx context.Context, gameID, resourceType, instanceID string) ([]entity.CredentialPool, error)
	// MarkOrphan 悬挂标记（占用实例失联等，admin 可 force-release）
	MarkOrphan(ctx context.Context, id string) error
	// ForceRelease 强制释放（orphan 或任意状态 → available，清占用）
	ForceRelease(ctx context.Context, id string) error
}
