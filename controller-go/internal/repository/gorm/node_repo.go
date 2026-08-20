package gorm

import (
	"context"
	"time"

	"controller-go/internal/entity"

	"gorm.io/gorm"
)

type NodeRepo struct {
	db *gorm.DB
}

func NewNodeRepo(db *gorm.DB) *NodeRepo {
	return &NodeRepo{db: db}
}

func (r *NodeRepo) Save(node *entity.Node) error {
	return r.db.Save(node).Error
}

func (r *NodeRepo) GetByID(id string) (*entity.Node, error) {
	var node entity.Node
	err := r.db.First(&node, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &node, nil
}

func (r *NodeRepo) ListAll(ctx context.Context) ([]*entity.Node, error) {
	var nodes []*entity.Node
	err := r.db.WithContext(ctx).Find(&nodes).Error
	if err != nil {
		return nil, err
	}
	return nodes, nil
}

func (r *NodeRepo) UpdateDynamicUsage(ctx context.Context, nodeID string, u entity.NodeDynamicUsage, reportedAt time.Time) error {
	return r.db.WithContext(ctx).Model(&entity.Node{}).
		Where("id = ?", nodeID).
		Updates(map[string]any{
			"cpu_used_milli":    u.CPUUsedMilli,
			"memory_used_bytes": u.MemoryUsedBytes,
			"disk_used_bytes":   u.DiskUsedBytes,
			"net_rx_bps":        u.NetRxBps,
			"net_tx_bps":        u.NetTxBps,
			"usage_reported_at": reportedAt,
		}).Error
}

func (r *NodeRepo) UpdatePressureStatus(ctx context.Context, nodeID string, status entity.NodePressureStatus) error {
	return r.db.WithContext(ctx).Model(&entity.Node{}).
		Where("id = ?", nodeID).
		Update("pressure_status", status).Error
}

func (r *NodeRepo) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&entity.Node{}, "id = ?", id).Error
}
