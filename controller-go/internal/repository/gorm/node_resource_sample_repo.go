package gorm

import (
	"context"
	"time"

	"controller-go/internal/entity"

	"gorm.io/gorm"
)

type NodeResourceSampleRepo struct {
	db *gorm.DB
}

func NewNodeResourceSampleRepo(db *gorm.DB) *NodeResourceSampleRepo {
	return &NodeResourceSampleRepo{db: db}
}

func (r *NodeResourceSampleRepo) Append(ctx context.Context, s *entity.NodeResourceSample) error {
	return r.db.WithContext(ctx).Create(s).Error
}

func (r *NodeResourceSampleRepo) ListSince(ctx context.Context, nodeID string, since time.Time) ([]*entity.NodeResourceSample, error) {
	var out []*entity.NodeResourceSample
	err := r.db.WithContext(ctx).
		Where("node_id = ? AND sampled_at >= ?", nodeID, since).
		Order("sampled_at ASC").
		Find(&out).Error
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *NodeResourceSampleRepo) PruneBefore(ctx context.Context, before time.Time) (int64, error) {
	res := r.db.WithContext(ctx).
		Where("sampled_at < ?", before).
		Delete(&entity.NodeResourceSample{})
	return res.RowsAffected, res.Error
}
