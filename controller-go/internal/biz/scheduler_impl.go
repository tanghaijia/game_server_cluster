package biz

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"controller-go/internal/entity"
	"controller-go/internal/repository"
)

// ResourceAwareScheduler 资源感知调度器（替换 SimpleScheduler）：
// filter（H1-H6）→ score（P1-P4）→ 预留事务绑定（§2.2/§7.1）。
// P1 阶段：可恢复原因（资源/端口/压力不足）暂返回 OutcomeFailed（FailCodeResourceShortage），
// P2 引入排队后改为 OutcomeQueued。
type ResourceAwareScheduler struct {
	nodeAgentRepo       repository.NodeAgentRepository
	nodeRepo            repository.NodeRepository
	sampleRepo          repository.NodeResourceSampleRepository
	reservationRepo     repository.ReservationRepository
	gameRepo            repository.GameRepository
	containerConfigRepo repository.GameContainerConfigRepository
	steamBranchRepo     repository.SteamBranchRepository
	portMapper          *GameContainerPortMapper
	cacheProvider       CacheStatusProvider

	weights           ScoreWeights
	utilizationTarget float64
	regionForce       bool
	reservationRetry  int
	historyWindow     time.Duration
	healthStaleWindow time.Duration

	// 统计（debug/指标，S29）
	mu       sync.Mutex
	attempts map[string]int64
}

func NewResourceAwareScheduler(
	nodeAgentRepo repository.NodeAgentRepository,
	nodeRepo repository.NodeRepository,
	sampleRepo repository.NodeResourceSampleRepository,
	reservationRepo repository.ReservationRepository,
	gameRepo repository.GameRepository,
	containerConfigRepo repository.GameContainerConfigRepository,
	steamBranchRepo repository.SteamBranchRepository,
	portMapper *GameContainerPortMapper,
	cacheProvider CacheStatusProvider,
	weights ScoreWeights,
	utilizationTarget float64,
	regionForce bool,
	reservationRetry int,
	historyWindow time.Duration,
	healthStaleWindow time.Duration,
) *ResourceAwareScheduler {
	if weights == (ScoreWeights{}) {
		weights = DefaultScoreWeights()
	}
	if utilizationTarget <= 0 || utilizationTarget > 1 {
		utilizationTarget = defaultUtilizationTarget
	}
	if reservationRetry <= 0 {
		reservationRetry = 3
	}
	if historyWindow <= 0 {
		historyWindow = 15 * time.Minute
	}
	if healthStaleWindow <= 0 {
		healthStaleWindow = 30 * time.Second
	}
	return &ResourceAwareScheduler{
		nodeAgentRepo:       nodeAgentRepo,
		nodeRepo:            nodeRepo,
		sampleRepo:          sampleRepo,
		reservationRepo:     reservationRepo,
		gameRepo:            gameRepo,
		containerConfigRepo: containerConfigRepo,
		steamBranchRepo:     steamBranchRepo,
		portMapper:          portMapper,
		cacheProvider:       cacheProvider,
		weights:             weights,
		utilizationTarget:   utilizationTarget,
		regionForce:         regionForce,
		reservationRetry:    reservationRetry,
		historyWindow:       historyWindow,
		healthStaleWindow:   healthStaleWindow,
		attempts:            make(map[string]int64),
	}
}

// resolveRequest 解析实例资源需求（3.1）：实例显式 > config 默认 > 系统默认
func (s *ResourceAwareScheduler) resolveRequest(instance *entity.GameInstance, config *entity.GameContainerConfig) entity.ResourceRequest {
	req := entity.ResourceRequest{
		CPUMilli:        config.CPURequestMilli,
		MemoryBytes:     config.MemoryRequestBytes,
		DiskBytes:       config.DiskRequestBytes,
		BandwidthRxMbps: config.BandwidthRxMbps,
		BandwidthTxMbps: config.BandwidthTxMbps,
	}
	if instance.ResourceReq != nil {
		if instance.ResourceReq.CPUMilli > 0 {
			req.CPUMilli = instance.ResourceReq.CPUMilli
		}
		if instance.ResourceReq.MemoryBytes > 0 {
			req.MemoryBytes = instance.ResourceReq.MemoryBytes
		}
		if instance.ResourceReq.DiskBytes > 0 {
			req.DiskBytes = instance.ResourceReq.DiskBytes
		}
		if instance.ResourceReq.BandwidthRxMbps > 0 {
			req.BandwidthRxMbps = instance.ResourceReq.BandwidthRxMbps
		}
		if instance.ResourceReq.BandwidthTxMbps > 0 {
			req.BandwidthTxMbps = instance.ResourceReq.BandwidthTxMbps
		}
	}
	// 兜底默认
	if req.CPUMilli <= 0 {
		req.CPUMilli = 1000
	}
	if req.MemoryBytes <= 0 {
		req.MemoryBytes = 1 << 30
	}
	if req.DiskBytes <= 0 {
		req.DiskBytes = 10 << 30
	}
	return req
}

// resolveBranch 解析实例 game_build 对应的 steam 分支名（H5 判定用）。
// 找不到返回空串（视为无缓存，节点全部被 H5 排除）。
func (s *ResourceAwareScheduler) resolveBranch(ctx context.Context, instance *entity.GameInstance) string {
	branches, err := s.steamBranchRepo.ListByGame(ctx, instance.GameID)
	if err != nil {
		return ""
	}
	for _, b := range branches {
		if fmt.Sprintf("%d", b.LastBuildId) == instance.GameBuildId {
			return b.BranchName
		}
	}
	return ""
}

func (s *ResourceAwareScheduler) Schedule(ctx context.Context, instance *entity.GameInstance) (*ScheduleResult, error) {
	game, err := s.gameRepo.GetByID(ctx, instance.GameID)
	if err != nil {
		return nil, fmt.Errorf("load game: %w", err)
	}
	config, err := s.containerConfigRepo.GetByID(ctx, game.ContainerConfigID)
	if err != nil {
		return nil, fmt.Errorf("load container config: %w", err)
	}
	req := s.resolveRequest(instance, config)

	// 单核应用 request 声明校验（3.1 声明规范）：整核且 ≥ 1000m
	if config.SingleThreaded && (req.CPUMilli < 1000 || req.CPUMilli%1000 != 0) {
		s.record("failed")
		return failed(FailCodeConfigError, "单核应用 cpu_milli 必须为整核（≥1000 且 %1000==0）"), nil
	}

	branch := s.resolveBranch(ctx, instance)

	// 重试预留冲突（§2.2 步骤 6）
	for attempt := 0; attempt <= s.reservationRetry; attempt++ {
		candidates, err := s.loadCandidates(ctx)
		if err != nil {
			return nil, err
		}

		// filter H1-H6
		var eligible []*NodeCandidate
		allResourceOnly := true
		for _, c := range candidates {
			if s.applyConstraints(ctx, c, instance, game, branch, req) {
				eligible = append(eligible, c)
			} else if !isResourceOnlyReasons(c.Reasons) {
				allResourceOnly = false
			}
		}

		if len(eligible) == 0 {
			return s.classifyNoCandidate(instance, candidates, allResourceOnly, branch), nil
		}

		// score → 选最优（确定性：同分取序号小者，候选按 ListAll 稳定顺序）
		best := s.pickBest(ctx, eligible, instance, config)

		// 预留事务（§7.1）：端口映射事务外预计算（H4 已通过，此处防竞态重算）
		mappings, err := s.portMapper.PlanPorts(ctx, best.Agent, game, instance)
		if err != nil {
			slog.Warn("[Scheduler] PlanPorts 竞态失败，重试",
				"instanceId", instance.ID, "nodeAgentId", best.Agent.ID, "err", err)
			continue
		}

		err = s.reservationRepo.TryReserve(ctx, repository.ReserveTxRequest{
			NodeID:            fmtID(best.Node.Id),
			InstanceID:        instance.ID,
			NodeAgentID:       best.Agent.ID,
			Req:               req,
			PortMappings:      mappings,
			NewStatus:         entity.StatusPreparingBuild,
			UtilizationTarget: s.utilizationTarget,
		})
		if errors.Is(err, repository.ErrReservationConflict) {
			slog.Warn("[Scheduler] 预留冲突（并发被抢占），重试",
				"instanceId", instance.ID, "attempt", attempt)
			continue
		}
		if err != nil {
			return nil, err
		}

		s.record("scheduled")
		return &ScheduleResult{
			Outcome:     OutcomeScheduled,
			NodeAgentID: best.Agent.ID,
			Score:       best.Score,
			Scores:      map[string]float64{"score": best.Score},
			Excluded:    buildExclusions(candidates),
			ResourceReq: req,
		}, nil
	}

	s.record("failed")
	return failed(FailCodeResourceShortage, "预留冲突重试达上限"), nil
}

// classifyNoCandidate 无候选时的决策出口（§2.3）：
// P1 阶段可恢复原因统一返回 Failed(ResourceShortage)，P2 改为 Queued。
func (s *ResourceAwareScheduler) classifyNoCandidate(
	instance *entity.GameInstance,
	candidates []*NodeCandidate,
	allResourceOnly bool,
	branch string,
) *ScheduleResult {
	excluded := buildExclusions(candidates)

	// 结构性原因优先判定
	if len(candidates) == 0 {
		s.record("failed")
		return &ScheduleResult{Outcome: OutcomeFailed, ReasonCode: FailCodeNoNodeAgent,
			Reason: "无注册 node_agent", Excluded: excluded}
	}
	if branch == "" {
		s.record("failed")
		return &ScheduleResult{Outcome: OutcomeFailed, ReasonCode: FailCodeNoGameCache,
			Reason: "无法解析 game_build 分支（无节点有缓存）", Excluded: excluded}
	}
	if s.regionForce && !hasRegionMatch(candidates, instance.Region) {
		s.record("failed")
		return &ScheduleResult{Outcome: OutcomeFailed, ReasonCode: FailCodeRegionUnreachable,
			Reason: "区域强制模式下无区域 " + instance.Region + " 的节点", Excluded: excluded}
	}
	if allResourceOnly {
		// P1：可恢复原因暂按失败处理（P2 改为 OutcomeQueued）
		s.record("queued")
		return &ScheduleResult{Outcome: OutcomeFailed, ReasonCode: FailCodeResourceShortage,
			Reason: "资源/端口不足，无可调度节点", Excluded: excluded}
	}
	s.record("failed")
	return &ScheduleResult{Outcome: OutcomeFailed, ReasonCode: FailCodeNoGameCache,
		Reason: "存在结构性排除原因（无缓存/未启用/不健康等）", Excluded: excluded}
}

// pickBest 评分排序取最优（§6.2）
func (s *ResourceAwareScheduler) pickBest(
	ctx context.Context,
	eligible []*NodeCandidate,
	instance *entity.GameInstance,
	config *entity.GameContainerConfig,
) *NodeCandidate {
	// 单核主频归一化参考
	minFreq, maxFreq := freqRange(eligible)

	for _, c := range eligible {
		in := ScoreInput{
			RegionMatch:      instance.Region != "" && c.Node.Location == instance.Region,
			LastNodeMatch:    instance.NodeAgentID != nil && *instance.NodeAgentID == c.Agent.ID,
			Utilization:      utilizationOf(c),
			HistoryUtil:      c.HistoryUtil,
			BandwidthRatio:   0, // P1 无带宽数据（P3 完善）
			Degraded:         c.Agent.HealthStatus == entity.HealthDegraded,
			SingleThreaded:   config.SingleThreaded,
			CoreFrequencyGHz: c.Node.CoreFrequency,
			MinFreqGHz:       minFreq,
			MaxFreqGHz:       maxFreq,
		}
		score, _ := ComputeScore(in, s.weights)
		c.Score = score
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		if eligible[i].Score != eligible[j].Score {
			return eligible[i].Score > eligible[j].Score
		}
		return eligible[i].Agent.ID < eligible[j].Agent.ID // 同分取序号小者（确定性）
	})
	return eligible[0]
}

// utilizationOf balance 评分输入：max(reserved, used) / capacity（§6.2 修正：取 max 防双重计算）
func utilizationOf(c *NodeCandidate) float64 {
	cpuCap := float64(cpuCapacityMilli(c.Node))
	if cpuCap <= 0 {
		return 0
	}
	reserved := float64(c.Node.CPUReservedMilli)
	used := float64(c.Node.CPUUsedMilli)
	if used > reserved {
		return used / cpuCap
	}
	return reserved / cpuCap
}

func freqRange(cands []*NodeCandidate) (min, max float64) {
	min, max = 0, 0
	for _, c := range cands {
		f := c.Node.CoreFrequency
		if min == 0 || f < min {
			min = f
		}
		if f > max {
			max = f
		}
	}
	return
}

func hasRegionMatch(cands []*NodeCandidate, region string) bool {
	if region == "" {
		return true // 任意区域视为匹配
	}
	for _, c := range cands {
		if c.Node.Location == region {
			return true
		}
	}
	return false
}

func buildExclusions(cands []*NodeCandidate) []NodeExclusion {
	out := make([]NodeExclusion, 0, len(cands))
	for _, c := range cands {
		if c.Excluded {
			out = append(out, NodeExclusion{NodeAgentID: c.Agent.ID, Reasons: c.Reasons})
		}
	}
	return out
}

func failed(code ScheduleFailCode, reason string) *ScheduleResult {
	return &ScheduleResult{Outcome: OutcomeFailed, ReasonCode: code, Reason: reason}
}

func (s *ResourceAwareScheduler) record(outcome string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts[outcome]++
}

// Stats 调度统计（debug/指标，S29）
func (s *ResourceAwareScheduler) Stats() map[string]int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]int64, len(s.attempts))
	for k, v := range s.attempts {
		out[k] = v
	}
	return out
}

// CancelQueued P2 实现；P1 阶段排队未启用，直接返回错误。
func (s *ResourceAwareScheduler) CancelQueued(ctx context.Context, instanceID string) error {
	return errors.New("queue not implemented in P1")
}

// QueueStats P2 实现；P1 阶段返回空统计。
func (s *ResourceAwareScheduler) QueueStats() map[string]any {
	return map[string]any{"queue_len": 0, "implemented": false}
}
