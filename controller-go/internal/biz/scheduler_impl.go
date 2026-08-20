package biz

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"controller-go/internal/entity"
	"controller-go/internal/repository"
)

// ResourceAwareScheduler 资源感知调度器（替换 SimpleScheduler）：
// filter（H1-H6）→ score（P1-P4）→ 预留事务绑定（§2.2/§7.1）。
// 决策出口（§2.3）：可恢复原因（资源/端口/压力不足）→ OutcomeQueued（P2 排队）；
// 结构性原因 → OutcomeFailed。
type ResourceAwareScheduler struct {
	nodeAgentRepo       repository.NodeAgentRepository
	nodeRepo            repository.NodeRepository
	instanceRepo        repository.GameInstanceRepository
	sampleRepo          repository.NodeResourceSampleRepository
	reservationRepo     repository.ReservationRepository
	gameRepo            repository.GameRepository
	containerConfigRepo repository.GameContainerConfigRepository
	steamBranchRepo     repository.SteamBranchRepository
	portMapper          *GameContainerPortMapper
	cacheProvider       CacheStatusProvider
	queueManager        *QueueManager
	eventBus            *SchedulerEventBus
	statRepo            repository.SchedulerStatRepository // 000023：调度统计持久化（重启不归零）

	weights           ScoreWeights
	utilizationTarget float64
	regionForce       bool
	reservationRetry  int
	historyWindow     time.Duration
	healthStaleWindow time.Duration

	// 统计（debug/指标，S29）：内存仅作实时快速读与无 DB 时的兜底，权威在 statRepo
	mu       sync.Mutex
	attempts map[string]int64
}

func NewResourceAwareScheduler(
	nodeAgentRepo repository.NodeAgentRepository,
	nodeRepo repository.NodeRepository,
	instanceRepo repository.GameInstanceRepository,
	sampleRepo repository.NodeResourceSampleRepository,
	reservationRepo repository.ReservationRepository,
	gameRepo repository.GameRepository,
	containerConfigRepo repository.GameContainerConfigRepository,
	steamBranchRepo repository.SteamBranchRepository,
	portMapper *GameContainerPortMapper,
	cacheProvider CacheStatusProvider,
	queueManager *QueueManager,
	eventBus *SchedulerEventBus,
	statRepo repository.SchedulerStatRepository,
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
		instanceRepo:        instanceRepo,
		sampleRepo:          sampleRepo,
		reservationRepo:     reservationRepo,
		gameRepo:            gameRepo,
		containerConfigRepo: containerConfigRepo,
		steamBranchRepo:     steamBranchRepo,
		portMapper:          portMapper,
		cacheProvider:       cacheProvider,
		queueManager:        queueManager,
		eventBus:            eventBus,
		statRepo:            statRepo,
		weights:             weights,
		utilizationTarget:   utilizationTarget,
		regionForce:         regionForce,
		reservationRetry:    reservationRetry,
		historyWindow:       historyWindow,
		healthStaleWindow:   healthStaleWindow,
		attempts:            make(map[string]int64),
	}
}

// resolveRequest 解析实例资源需求（3.1）：
// 优先级 = 创建时显式指定（ResourceOverride=true）> game_container_config 当前值 > 系统默认。
// 调度成功写回的快照（ResourceReq，ResourceOverride=false）仅用于释放预留，不覆盖 config 后续变更
// （否则修改 config 不会影响已创建实例的调度预留）。
func (s *ResourceAwareScheduler) resolveRequest(instance *entity.GameInstance, config *entity.GameContainerConfig) entity.ResourceRequest {
	req := entity.ResourceRequest{
		CPUMilli:        config.CPURequestMilli,
		MemoryBytes:     config.MemoryRequestBytes,
		DiskBytes:       config.DiskRequestBytes,
		BandwidthRxMbps: config.BandwidthRxMbps,
		BandwidthTxMbps: config.BandwidthTxMbps,
	}
	// 仅创建时显式指定才覆盖 config（000021）
	if instance.ResourceReq != nil && instance.ResourceOverride {
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
// 1) 优先：分支 last_build_id 精确匹配实例 build；
// 2) 回退：该 game 任一 Enable 分支（缓存按 (game, branch) 组织，build 版本差异由启动流程处理；
//    避免"分支表有记录但 build 非最新"时误判无缓存导致调度失败）。
// 分支表完全没有该 game 的 Enable 记录时返回空串（视为无缓存，需先同步分支）。
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
	for _, b := range branches {
		if b.Status == entity.Enable {
			return b.BranchName
		}
	}
	return ""
}

func (s *ResourceAwareScheduler) Schedule(ctx context.Context, instance *entity.GameInstance) (*ScheduleResult, error) {
	start := time.Now()
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
		s.publishEvent(EventInstanceScheduleFailed, instance.ID, "", "单核应用 cpu_milli 必须为整核（≥1000 且 %1000==0）")
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
			res := s.classifyNoCandidate(instance, candidates, allResourceOnly, branch)
			if res.Outcome == OutcomeQueued {
				s.publishEvent(EventInstanceQueued, instance.ID, "", res.Reason)
			} else {
				s.publishEvent(EventInstanceScheduleFailed, instance.ID, "", res.Reason)
			}
			s.audit(instance, res, time.Since(start))
			return res, nil
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
		s.publishEvent(EventInstanceScheduled, instance.ID, best.Agent.ID,
			fmt.Sprintf("score=%.2f", best.Score))
		slog.Info("[Scheduler] 预留扣减",
			"instanceId", instance.ID, "nodeAgentId", best.Agent.ID,
			"nodeId", fmtID(best.Node.Id),
			"cpuMilli", req.CPUMilli, "memBytes", req.MemoryBytes, "diskBytes", req.DiskBytes,
			"bwRxMbps", req.BandwidthRxMbps, "bwTxMbps", req.BandwidthTxMbps)
		res := &ScheduleResult{
			Outcome:     OutcomeScheduled,
			NodeAgentID: best.Agent.ID,
			Score:       best.Score,
			Scores:      map[string]float64{"score": best.Score},
			Excluded:    buildExclusions(candidates),
			ResourceReq: req,
		}
		s.audit(instance, res, time.Since(start))
		return res, nil
	}

	s.record("failed")
	return failed(FailCodeResourceShortage, "预留冲突重试达上限"), nil
}

// publishEvent 发布调度事件（观测 S30；eventBus 为空时忽略）
func (s *ResourceAwareScheduler) publishEvent(typ SchedulerEventType, instanceID, nodeAgentID, detail string) {
	if s.eventBus == nil {
		return
	}
	s.eventBus.Publish(SchedulerEvent{
		Type:        typ,
		OccurredAt:  time.Now(),
		InstanceID:  instanceID,
		NodeAgentID: nodeAgentID,
		Detail:      detail,
	})
}

// audit 调度审计日志（F2/S28）：输入需求、出口、得分、排除明细、耗时
func (s *ResourceAwareScheduler) audit(instance *entity.GameInstance, res *ScheduleResult, dur time.Duration) {
	excluded := make([]string, 0, len(res.Excluded))
	for _, e := range res.Excluded {
		excluded = append(excluded, e.NodeAgentID+":"+strings.Join(e.Reasons, ";"))
	}
	slog.Info("[Scheduler] 调度决策",
		"instanceId", instance.ID,
		"game", instance.GameID,
		"region", instance.Region,
		"outcome", res.Outcome,
		"reason_code", res.ReasonCode,
		"reason", res.Reason,
		"node_agent_id", res.NodeAgentID,
		"score", res.Score,
		"excluded", strings.Join(excluded, " | "),
		"duration_ms", dur.Milliseconds(),
	)
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
		// 可恢复原因（资源/端口/压力不足）→ 排队（R8，P2）
		s.record("queued")
		return &ScheduleResult{Outcome: OutcomeQueued, ReasonCode: FailCodeResourceShortage,
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
			BandwidthRatio:   c.BandwidthRatio, // §3.5 带宽余量（P3 已接入）
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

// record 记录一次调度出口（S29 指标）：
// 内存 map 累加（实时快速读/兜底）+ 双写 DB（scheduler_stats 表，重启不归零）。
// DB 写失败仅记日志，不影响调度热路径。
func (s *ResourceAwareScheduler) record(outcome string) {
	s.mu.Lock()
	s.attempts[outcome]++
	s.mu.Unlock()

	if s.statRepo != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := s.statRepo.Incr(ctx, outcome); err != nil {
			slog.Error("[Scheduler] 统计持久化失败", "outcome", outcome, "err", err)
		}
	}
}

// Stats 调度统计（debug/指标，S29）：DB 持久化值为权威（重启后不归零）。
// 若 DB 不可用回退内存快照。内存增量与 DB 的关系：每次 record 双写，
// 因此 DB 已包含全部历史与当前计数，内存仅作兜底。
func (s *ResourceAwareScheduler) Stats() map[string]int64 {
	if s.statRepo != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if m, err := s.statRepo.All(ctx); err == nil && len(m) > 0 {
			return m
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]int64, len(s.attempts))
	for k, v := range s.attempts {
		out[k] = v
	}
	return out
}

// PreviewNode 试调度单个节点的判定结果（观测用，F2 可解释性）
type PreviewNode struct {
	NodeAgentID string   `json:"node_agent_id"`
	NodeID      string   `json:"node_id"`
	IP          string   `json:"ip"`
	Location    string   `json:"location"`
	Eligible    bool     `json:"eligible"`          // 通过全部硬约束 H1-H6
	Reasons     []string `json:"reasons,omitempty"` // 排除原因明细
	Score       float64  `json:"score"`             // 软偏好得分（eligible 时有效）
}

// PreviewResult 试调度结果（观测/调试）
type PreviewResult struct {
	Outcome  ScheduleOutcome `json:"outcome"` // scheduled / queued / failed
	Reason   string          `json:"reason"`
	Selected string          `json:"selected,omitempty"` // 最优节点（scheduled 时）
	Nodes    []PreviewNode   `json:"nodes"`              // 全部候选判定明细
}

// Preview 试调度干跑（观测/调试）：执行 filter(H1-H6) + score 但不预留、不落库、不污染统计。
// 管理员可借此理解"为什么选/不选某节点"（F2）。
func (s *ResourceAwareScheduler) Preview(ctx context.Context, instance *entity.GameInstance) (*PreviewResult, error) {
	game, err := s.gameRepo.GetByID(ctx, instance.GameID)
	if err != nil {
		return nil, fmt.Errorf("load game: %w", err)
	}
	config, err := s.containerConfigRepo.GetByID(ctx, game.ContainerConfigID)
	if err != nil {
		return nil, fmt.Errorf("load container config: %w", err)
	}
	req := s.resolveRequest(instance, config)
	branch := s.resolveBranch(ctx, instance)

	candidates, err := s.loadCandidates(ctx)
	if err != nil {
		return nil, err
	}

	res := &PreviewResult{Nodes: make([]PreviewNode, 0, len(candidates))}
	var eligible []*NodeCandidate
	for _, c := range candidates {
		pn := PreviewNode{
			NodeAgentID: c.Agent.ID,
			NodeID:      fmtID(c.Node.Id),
			IP:          c.Node.Ip,
			Location:    c.Node.Location,
		}
		if s.applyConstraints(ctx, c, instance, game, branch, req) {
			pn.Eligible = true
			eligible = append(eligible, c)
		} else {
			pn.Reasons = c.Reasons
		}
		res.Nodes = append(res.Nodes, pn)
	}

	if len(eligible) == 0 {
		allResourceOnly := true
		for _, c := range candidates {
			if c.Excluded && !isResourceOnlyReasons(c.Reasons) {
				allResourceOnly = false
				break
			}
		}
		if allResourceOnly {
			res.Outcome = OutcomeQueued
			res.Reason = "资源/端口不足（可排队，P2 等待唤醒）"
		} else {
			res.Outcome = OutcomeFailed
			res.Reason = "存在结构性排除原因（无缓存/未启用/不健康/区域强制等）"
		}
		return res, nil
	}

	best := s.pickBest(ctx, eligible, instance, config)
	for i := range res.Nodes {
		if res.Nodes[i].NodeAgentID == best.Agent.ID {
			res.Nodes[i].Score = best.Score
		}
	}
	res.Outcome = OutcomeScheduled
	res.Selected = best.Agent.ID
	return res, nil
}

// CancelQueued 取消排队（D5 + D10）：仅 queued 状态允许；
// 移除队列行 + 实例置 stopped + cancelled 标记（幂等：非排队状态返回 ErrNotQueued）。
func (s *ResourceAwareScheduler) CancelQueued(ctx context.Context, instanceID string) error {
	inst, err := s.instanceRepo.GetByID(ctx, instanceID)
	if err != nil {
		return err
	}
	if inst.Status != entity.StatusQueued {
		return ErrNotQueued
	}
	if err := s.queueManager.Cancel(ctx, instanceID); err != nil {
		return err
	}
	inst.Status = entity.StatusStopped
	inst.Cancelled = true
	inst.QueuedReason = ""
	inst.QueuedAt = nil
	s.publishEvent(EventInstanceQueuedCancelled, instanceID, "", "用户取消排队")
	return s.instanceRepo.Save(ctx, inst)
}

// QueueStats 排队统计（debug/指标，S29）
func (s *ResourceAwareScheduler) QueueStats() map[string]any {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	n, err := s.queueManager.Count(ctx)
	if err != nil {
		return map[string]any{"queue_len": 0, "implemented": true, "error": err.Error()}
	}
	return map[string]any{"queue_len": n, "implemented": true}
}

// ErrNotQueued 实例不在排队状态（取消时状态守卫拒绝）
var ErrNotQueued = errors.New("instance is not in queued state")
