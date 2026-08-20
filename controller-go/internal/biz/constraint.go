package biz

import (
	"context"
	"strings"
	"time"

	"controller-go/internal/entity"
)

// 硬约束 H1-H6（§4.1/§6.1）。
// 返回该候选是否通过全部硬约束；未通过时 c.Reasons 已填充排除原因。

func (s *ResourceAwareScheduler) applyConstraints(
	ctx context.Context,
	c *NodeCandidate,
	instance *entity.GameInstance,
	game *entity.Game,
	branch string,
	req entity.ResourceRequest,
) bool {
	if !s.constraintEnabled(c) {
		return false
	}
	if !s.constraintHealth(c) {
		return false
	}
	if !s.constraintResources(c, req) {
		return false
	}
	if !s.constraintPorts(ctx, c, instance, game) {
		return false
	}
	if !s.constraintCache(ctx, c, instance, branch) {
		return false
	}
	if !s.constraintPressure(c) {
		return false
	}
	return true
}

// constraintEnabled H1：node_agent 为 Enabled
func (s *ResourceAwareScheduler) constraintEnabled(c *NodeCandidate) bool {
	if c.Agent.Status != entity.Enabled {
		c.Excluded = true
		c.Reasons = append(c.Reasons, "node_agent 未启用")
		return false
	}
	return true
}

// constraintHealth H2：health ∈ {healthy, degraded} 且心跳新鲜（§9.2）
func (s *ResourceAwareScheduler) constraintHealth(c *NodeCandidate) bool {
	switch c.Agent.HealthStatus {
	case entity.HealthHealthy, entity.HealthDegraded:
		if c.Agent.LastHeartbeatAt == nil {
			c.Excluded = true
			c.Reasons = append(c.Reasons, "健康状态未知（无心跳记录）")
			return false
		}
		if time.Since(*c.Agent.LastHeartbeatAt) > s.healthStaleWindow {
			c.Excluded = true
			c.Reasons = append(c.Reasons, "心跳过期（数据不可信）")
			return false
		}
		return true
	default:
		// unknown（未探测）/ unhealthy
		c.Excluded = true
		c.Reasons = append(c.Reasons, "健康状态不可调度（unknown/unhealthy）")
		return false
	}
}

// constraintResources H3：预留视图 allocatable ≥ request（逐维，附缺口量）
func (s *ResourceAwareScheduler) constraintResources(c *NodeCandidate, req entity.ResourceRequest) bool {
	if c.Capacity.ResourcesSatisfied(req) {
		return true
	}
	c.Excluded = true
	c.Reasons = append(c.Reasons, c.Capacity.Shortages(req)...)
	return false
}

// constraintPorts H4：端口需求可满足（PlanPorts 预检，只查不写）
func (s *ResourceAwareScheduler) constraintPorts(ctx context.Context, c *NodeCandidate, instance *entity.GameInstance, game *entity.Game) bool {
	if _, err := s.portMapper.PlanPorts(ctx, c.Agent, game, instance); err != nil {
		c.Excluded = true
		c.Reasons = append(c.Reasons, "端口不足/不可分配: "+err.Error())
		return false
	}
	return true
}

// constraintCache H5：该 (game, branch) 在节点上为 AVAILABLE（D2：DOWNLOADING 不算命中）
func (s *ResourceAwareScheduler) constraintCache(ctx context.Context, c *NodeCandidate, instance *entity.GameInstance, branch string) bool {
	if branch == "" {
		c.Excluded = true
		c.Reasons = append(c.Reasons, "无法解析实例 game_build 对应分支（视为无缓存）")
		return false
	}
	ok, err := s.cacheProvider.CacheAvailable(ctx, c.Agent.ID, instance.GameID, branch)
	if err != nil {
		c.Excluded = true
		c.Reasons = append(c.Reasons, "game-cache 查询失败: "+err.Error())
		return false
	}
	if !ok {
		c.Excluded = true
		c.Reasons = append(c.Reasons, "无该 game_build 的 AVAILABLE 缓存")
		return false
	}
	return true
}

// constraintPressure H6：节点非压力状态（3.3；Warning/Critical 排除）
func (s *ResourceAwareScheduler) constraintPressure(c *NodeCandidate) bool {
	if c.Node.PressureStatus == entity.PressureNormal {
		return true
	}
	c.Excluded = true
	c.Reasons = append(c.Reasons, "节点压力状态（"+pressureStatusName(c.Node.PressureStatus)+"）")
	return false
}

func pressureStatusName(s entity.NodePressureStatus) string {
	switch s {
	case entity.PressureNormal:
		return "Normal"
	case entity.PressureWarning:
		return "Warning"
	case entity.PressureCritical:
		return "Critical"
	default:
		return "Unknown"
	}
}

// isResourceOnlyReasons 判断排除原因是否全部属于"可恢复资源类"
// （仅资源/端口/压力不足 → 可排队；含结构性原因 → 失败）。
func isResourceOnlyReasons(reasons []string) bool {
	for _, r := range reasons {
		if strings.Contains(r, "未启用") ||
			strings.Contains(r, "健康") ||
			strings.Contains(r, "缓存") ||
			strings.Contains(r, "无法解析") ||
			strings.Contains(r, "分支") {
			return false
		}
	}
	return true
}
