package biz

import (
	"fmt"

	"controller-go/internal/entity"
)

// 单位换算与容量视图（§3.2，H3 判定基础）
// 容量语义：nodes 容量字段 = 平台可分配上限（非物理总量），系统开销由运维配置时扣除。

const (
	mebibyte = 1024 * 1024
	// defaultUtilizationTarget 默认利用率目标：headroom = capacity × (1 − target)（S31）
	defaultUtilizationTarget = 0.8
)

// cpuCapacityMilli 节点 CPU 容量（milli 核）：core_num × 1000；
// core_frequency 不乘进容量（§3.2），仅评分参考（单核主频偏好）。
func cpuCapacityMilli(n *entity.Node) int64 {
	return int64(n.CoreNum) * 1000
}

// memoryCapacityBytes 节点内存容量（bytes）：memory_size 单位 MB
func memoryCapacityBytes(n *entity.Node) int64 {
	return n.MemorySize * mebibyte
}

// diskCapacityBytes 节点磁盘容量（bytes）：storage_size 单位 MB
func diskCapacityBytes(n *entity.Node) int64 {
	return n.StorageSize * mebibyte
}

// capacityLimit 考虑 utilization_target 后的可分配上限
func capacityLimit(capacity int64, target float64) int64 {
	if target <= 0 || target > 1 {
		target = defaultUtilizationTarget
	}
	return int64(float64(capacity) * target)
}

// NodeCapacity 节点容量视图（① 预留视图，§3.2）
type NodeCapacity struct {
	CPUAllocatableMilli  int64
	MemAllocatableBytes  int64
	DiskAllocatableBytes int64
}

// ComputeCapacity allocatable = capacity × utilization_target − reserved
func ComputeCapacity(n *entity.Node, target float64) NodeCapacity {
	return NodeCapacity{
		CPUAllocatableMilli:  capacityLimit(cpuCapacityMilli(n), target) - n.CPUReservedMilli,
		MemAllocatableBytes:  capacityLimit(memoryCapacityBytes(n), target) - n.MemoryReservedBytes,
		DiskAllocatableBytes: capacityLimit(diskCapacityBytes(n), target) - n.DiskReservedBytes,
	}
}

// ResourcesSatisfied 逐维判定 allocatable ≥ request（H3）
func (c NodeCapacity) ResourcesSatisfied(req entity.ResourceRequest) bool {
	return c.CPUAllocatableMilli >= req.CPUMilli &&
		c.MemAllocatableBytes >= req.MemoryBytes &&
		c.DiskAllocatableBytes >= req.DiskBytes
}

// Shortages 返回不满足维度的缺口明细（F1 排除原因）
func (c NodeCapacity) Shortages(req entity.ResourceRequest) []string {
	var out []string
	if c.CPUAllocatableMilli < req.CPUMilli {
		out = append(out, fmt.Sprintf("cpu 不足: 缺 %dm", req.CPUMilli-c.CPUAllocatableMilli))
	}
	if c.MemAllocatableBytes < req.MemoryBytes {
		out = append(out, fmt.Sprintf("内存不足: 缺 %d bytes", req.MemoryBytes-c.MemAllocatableBytes))
	}
	if c.DiskAllocatableBytes < req.DiskBytes {
		out = append(out, fmt.Sprintf("磁盘不足: 缺 %d bytes", req.DiskBytes-c.DiskAllocatableBytes))
	}
	return out
}
