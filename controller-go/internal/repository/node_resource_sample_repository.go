package repository

import (
	"context"
	"time"

	"controller-go/internal/entity"
)

// NodeResourceSampleRepository 节点资源采样（000013 表，历史视图数据源，§3.4）。
type NodeResourceSampleRepository interface {
	// Append 追加一条采样
	Append(ctx context.Context, s *entity.NodeResourceSample) error
	// ListSince 查询某节点自 since 起的采样（时间升序），用于评分窗口（均值/P95）与压力持续判定
	ListSince(ctx context.Context, nodeID string, since time.Time) ([]*entity.NodeResourceSample, error)
	// PruneBefore 清理早于保留窗口的采样（保留策略，如 7 天）
	PruneBefore(ctx context.Context, before time.Time) (int64, error)
}
