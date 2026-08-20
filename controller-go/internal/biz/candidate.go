package biz

import (
	"context"
	"strconv"
	"time"

	"controller-go/internal/entity"
)

// CacheStatusProvider 提供节点 game-cache 状态（H5 判定，§6.1/§10）。
// 由 GameCacheManager 实现（GetNodeCache 实时查询 node_agent）；P3 可替换为 NodeCacheView 快照。
type CacheStatusProvider interface {
	// CacheAvailable 判断节点是否有该 (game, branch) 的 AVAILABLE 缓存
	CacheAvailable(ctx context.Context, nodeAgentID, gameID, branchName string) (bool, error)
}

// NodeCandidate 调度候选节点（§2.2 步骤 1）
type NodeCandidate struct {
	Agent *entity.NodeAgent
	Node  *entity.Node

	Capacity NodeCapacity // 预留视图（H3）

	// filter 结果（constraint.go 填充）
	Excluded bool
	Reasons  []string

	// scoring 结果（scoring.go 填充）
	Score float64

	// 评分输入（candidate 组装时预计算）
	HistoryUtil     float64 // 窗口均值利用率 0..1
	BandwidthRatio  float64 // 带宽余量占比 0..1（P1 无带宽数据，恒 0）
}

// loadCandidates 组装候选视图：
// enabled node_agent × node × 容量视图 × 历史采样（§3.4） × 健康 × 压力 × 端口占用 × cache 快照。
// 端口占用与 cache 在 filter 阶段按需查询（H4/H5）。
func (s *ResourceAwareScheduler) loadCandidates(ctx context.Context) ([]*NodeCandidate, error) {
	agents, err := s.nodeAgentRepo.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	nodes, err := s.nodeRepo.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	nodeByID := make(map[string]*entity.Node, len(nodes))
	for _, n := range nodes {
		nodeByID[fmtID(n.Id)] = n
	}

	// 历史窗口起点（③ 历史视图，§3.4）
	since := time.Now().Add(-s.historyWindow)

	cands := make([]*NodeCandidate, 0, len(agents))
	for _, agent := range agents {
		node, ok := nodeByID[agent.NodeId]
		if !ok {
			continue // 无对应 node 记录，跳过（配置不完整）
		}
		c := &NodeCandidate{
			Agent:    agent,
			Node:     node,
			Capacity: ComputeCapacity(node, s.utilizationTarget),
		}
		// 历史利用率（评分 history_score；采样缺失视为 0 占用，保守不过分惩罚）
		if samples, err := s.sampleRepo.ListSince(ctx, fmtID(node.Id), since); err == nil && len(samples) > 0 {
			c.HistoryUtil = avgUtilization(samples, node)
		}
		cands = append(cands, c)
	}
	return cands, nil
}

// avgUtilization 窗口内 cpu/mem 均值利用率的较大者（history_score 输入）
func avgUtilization(samples []*entity.NodeResourceSample, node *entity.Node) float64 {
	if len(samples) == 0 {
		return 0
	}
	cpuCap := float64(cpuCapacityMilli(node))
	memCap := float64(memoryCapacityBytes(node))
	var cpuSum, memSum float64
	for _, s := range samples {
		cpuSum += float64(s.CPUUsedMilli)
		memSum += float64(s.MemoryUsedBytes)
	}
	n := float64(len(samples))
	cpu := cpuSum / n / cpuCap
	mem := memSum / n / memCap
	if cpu > mem {
		return cpu
	}
	return mem
}

// fmtID 节点 id 统一转字符串（nodes.id 为 bigserial 数值，agent.node_id 存字符串）
func fmtID(id int64) string {
	return strconv.FormatInt(id, 10)
}
