package gorm

import (
	"context"
	"errors"

	"controller-go/internal/entity"
	"controller-go/internal/repository"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ReservationRepo struct {
	db *gorm.DB
}

func NewReservationRepo(db *gorm.DB) *ReservationRepo {
	return &ReservationRepo{db: db}
}

// TryReserve 单事务预留（设计 §7.1，N4 预留与绑定同事务）：
// FOR UPDATE 锁节点行 → 复核 H3（预留视图 allocatable ≥ request）→ 扣减 reserved →
// 写端口映射 → 绑定实例（node_agent_id + status=PreparingBuild）。
// 复核不通过返回 repository.ErrReservationConflict（并发被抢占/资源变化），调用方重试或转排队。
func (r *ReservationRepo) TryReserve(ctx context.Context, req repository.ReserveTxRequest) error {
	target := req.UtilizationTarget
	if target <= 0 || target > 1 {
		target = 0.8
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 锁节点行（并发调度串行化）
		var node entity.Node
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&node, "id = ?", req.NodeID).Error; err != nil {
			return err
		}

		// 2. 复核 H3：容量(可分配上限 × utilization_target) − 已预留 ≥ request（3.2）
		cpuCap := int64(node.CoreNum) * 1000
		memCap := node.MemorySize * 1024 * 1024 // MB → bytes
		diskCap := node.StorageSize * 1024 * 1024
		if node.CPUReservedMilli+req.Req.CPUMilli > int64(float64(cpuCap)*target) ||
			node.MemoryReservedBytes+req.Req.MemoryBytes > int64(float64(memCap)*target) ||
			node.DiskReservedBytes+req.Req.DiskBytes > int64(float64(diskCap)*target) {
			return repository.ErrReservationConflict
		}

		// 3. 扣减预留
		if err := tx.Model(&entity.Node{}).Where("id = ?", req.NodeID).Updates(map[string]any{
			"cpu_reserved_milli":    gorm.Expr("cpu_reserved_milli + ?", req.Req.CPUMilli),
			"memory_reserved_bytes": gorm.Expr("memory_reserved_bytes + ?", req.Req.MemoryBytes),
			"disk_reserved_bytes":   gorm.Expr("disk_reserved_bytes + ?", req.Req.DiskBytes),
		}).Error; err != nil {
			return err
		}

		// 4. 写端口映射（biz 层事务外预计算的分配结果）
		for i := range req.PortMappings {
			if err := tx.Create(&req.PortMappings[i]).Error; err != nil {
				return err
			}
		}

		// 5. 绑定实例（node_agent_id + 进入 PreparingBuild）
		if err := tx.Model(&entity.GameInstance{}).Where("id = ?", req.InstanceID).
			Updates(map[string]any{
				"node_agent_id": req.NodeAgentID,
				"status":        req.NewStatus,
			}).Error; err != nil {
			return err
		}
		return nil
	})
	if errors.Is(err, repository.ErrReservationConflict) {
		return repository.ErrReservationConflict
	}
	return err
}

// Release 扣回预留（7.2 释放挂点：停止/删除/失败回滚/排队超时/卡死哨兵）。
// GREATEST(..., 0) 防负数（幂等释放安全）。
func (r *ReservationRepo) Release(ctx context.Context, nodeID string, req entity.ResourceRequest) error {
	return r.db.WithContext(ctx).Model(&entity.Node{}).
		Where("id = ?", nodeID).
		Updates(map[string]any{
			"cpu_reserved_milli":    gorm.Expr("GREATEST(cpu_reserved_milli - ?, 0)", req.CPUMilli),
			"memory_reserved_bytes": gorm.Expr("GREATEST(memory_reserved_bytes - ?, 0)", req.MemoryBytes),
			"disk_reserved_bytes":   gorm.Expr("GREATEST(disk_reserved_bytes - ?, 0)", req.DiskBytes),
		}).Error
}
