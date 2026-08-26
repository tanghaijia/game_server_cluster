package biz

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"controller-go/internal/client/assetservice"
	"controller-go/internal/client/nodeagent"
	"controller-go/internal/entity"
	"controller-go/internal/repository"
	assetservicev1 "controller-go/internal/third/assetservice/v1"
	nodeagentv1 "controller-go/internal/third/nodeagent/v1"

	"gorm.io/gorm"
)

type GameCacheManager struct {
	nodeAgentClients *nodeagent.ClientRegistry
	assetClient      *assetservice.AssetServiceFaceClient
	businessClient   *assetservice.BusinessServiceFaceClient
	steamBranchRepo  repository.SteamBranchRepository
	nodeAgentRepo    repository.NodeAgentRepository
	nodeRepo         repository.NodeRepository
	gameRepo         repository.GameRepository
	// P2-D：demand（实例承载视图）统计用
	instanceRepo repository.GameInstanceRepository

	// P2-D：删除冷却（§4.6 防抖）——branch 最近一次删除缓存的时间，冷却期内不删
	deleteMu       sync.Mutex
	deleteCooldown map[string]time.Time
}

func NewGameCacheManager(
	nodeAgentClients *nodeagent.ClientRegistry,
	assetClient *assetservice.AssetServiceFaceClient,
	businessClient *assetservice.BusinessServiceFaceClient,
	steamBranchRepo repository.SteamBranchRepository,
	nodeAgentRepo repository.NodeAgentRepository,
	nodeRepo repository.NodeRepository,
	gameRepo repository.GameRepository,
	instanceRepo repository.GameInstanceRepository,
) *GameCacheManager {
	return &GameCacheManager{
		nodeAgentClients: nodeAgentClients,
		assetClient:      assetClient,
		businessClient:   businessClient,
		steamBranchRepo:  steamBranchRepo,
		nodeAgentRepo:    nodeAgentRepo,
		nodeRepo:         nodeRepo,
		gameRepo:         gameRepo,
		instanceRepo:     instanceRepo,
		deleteCooldown:   make(map[string]time.Time),
	}
}

// cacheDeleteCooldown 删除冷却时长（§4.6 防抖）：demand 波动时避免"下完删、删完再下"
const cacheDeleteCooldown = 10 * time.Minute

// steamPublicBranchName Steam 默认分支名
const steamPublicBranchName = "public"

// steamBranchID 用 game_id 与 branch_name 组成稳定主键，保证重复同步幂等
func steamBranchID(gameId, branchName string) string {
	return gameId + ":" + branchName
}

/**
 * 调用asset_service的ListSteamBranches获取所有分支并记录到表中，
 * 若表中无分支但asset_service中有，public分支默认状态置Enable，其他分支默认置Disable，
 * 若表中有分支但asset_service中没有，视为废弃，置Abandoned
 */
func (g *GameCacheManager) SyncAndRecordBranch(ctx context.Context, gameId string) error {
	if gameId == "" {
		return errors.New("game_id is required")
	}

	resp, err := g.businessClient.ListSteamBranches(ctx, &assetservicev1.ListSteamBranchesRequest{GameId: gameId})
	if err != nil {
		return fmt.Errorf("list steam branches from asset_service: %w", err)
	}

	now := time.Now()
	remoteBranches := make(map[string]bool, len(resp.Branches))

	for _, b := range resp.Branches {
		if b == nil {
			continue
		}
		remoteBranches[b.Name] = true

		description := ""
		if b.Description != nil {
			description = *b.Description
		}

		existing, err := g.steamBranchRepo.GetByGameAndBranch(ctx, gameId, b.Name)
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			// 表中不存在 → 新增；public 分支默认 Enable，其他默认 Disable
			status := entity.Disable
			if b.Name == steamPublicBranchName {
				status = entity.Enable
			}
			branch := &entity.SteamBranch{
				Id:          steamBranchID(gameId, b.Name),
				BranchName:  b.Name,
				LastBuildId: b.BuildId,
				Description: description,
				GameId:      gameId,
				Status:      status,
				CreateTime:  now,
				UpdateTime:  now,
			}
			if err := g.steamBranchRepo.Save(ctx, branch); err != nil {
				return fmt.Errorf("save steam branch %s: %w", b.Name, err)
			}
		case err != nil:
			return fmt.Errorf("get steam branch %s: %w", b.Name, err)
		default:
			// 表中已存在 → 更新构建信息，保留原 status
			existing.LastBuildId = b.BuildId
			existing.Description = description
			existing.UpdateTime = now
			if err := g.steamBranchRepo.Save(ctx, existing); err != nil {
				return fmt.Errorf("update steam branch %s: %w", b.Name, err)
			}
		}
	}

	// 表中存在但 asset_service 已无该分支 → 视为废弃
	localBranches, err := g.steamBranchRepo.ListByGame(ctx, gameId)
	if err != nil {
		return fmt.Errorf("list local steam branches: %w", err)
	}
	for _, lb := range localBranches {
		if lb.Status == entity.Abandoned || remoteBranches[lb.BranchName] {
			continue
		}
		lb.Status = entity.Abandoned
		lb.UpdateTime = now
		if err := g.steamBranchRepo.Save(ctx, lb); err != nil {
			return fmt.Errorf("abandon steam branch %s: %w", lb.BranchName, err)
		}
	}

	slog.Info("[GameCacheManager] 分支同步完成",
		"gameId", gameId, "remote", len(resp.Branches), "local", len(localBranches))
	return nil
}

/*
 * 检查NodeAgent的GameCache版本，若小于最新版本(lastBuildId)，且状态是Available或Removed，调用cachegame启动下载，返回成功
 * 若版本是最新版本且状态Removed，启动下载，返回成功，
 * 若版本是最新版本且状态Available，返回成功，
 * 若状态是Downloading，返回成功，
 * 其他情况，返回错误
 */
func (g *GameCacheManager) CheckAndUpdate(ctx context.Context,
	gameId string, branchName string, lastBuildId uint64, nodeAgentId string) error {

	if gameId == "" || branchName == "" {
		return errors.New("game_id and branch_name are required")
	}
	if nodeAgentId == "" {
		return errors.New("node_agent_id is required")
	}

	// 1. 解析 NodeAgent 地址并获取客户端
	nodeAgent, err := g.nodeAgentRepo.GetByID(ctx, nodeAgentId)
	if err != nil {
		return fmt.Errorf("get node_agent %s: %w", nodeAgentId, err)
	}
	node, err := g.nodeRepo.GetByID(nodeAgent.NodeId)
	if err != nil {
		return fmt.Errorf("get node %s: %w", nodeAgent.NodeId, err)
	}
	client, err := g.nodeAgentClients.Get(ctx, nodeAgentId, fmt.Sprintf("%s:%d", node.Ip, nodeAgent.Port))
	if err != nil {
		return fmt.Errorf("get node_agent client %s: %w", nodeAgentId, err)
	}

	// 2. 查询 NodeAgent 当前的 GameCache
	resp, err := client.GetCacheGame(ctx, &nodeagentv1.GetCacheGameRequest{
		GameId:     gameId,
		BranchName: branchName,
	})
	if err != nil {
		// 缓存不存在 → 直接启动下载；其他错误原样返回
		if !isGameCacheNotFound(extractErrorDetail(err)) {
			return fmt.Errorf("get cache game from node_agent %s: %w", nodeAgentId, err)
		}
		return g.triggerDownload(ctx, client, nodeAgentId, gameId, branchName, lastBuildId)
	}
	if resp == nil || resp.GameCache == nil {
		return errors.New("node_agent returned empty game cache")
	}
	gc := resp.GameCache

	// 3. 先按状态判断：下载中幂等成功、不可用报错（无需比较版本）
	switch gc.GetStatus() {
	case nodeagentv1.GameCacheStatus_DOWNLOADING:
		slog.Info("[GameCacheManager] GameCache 下载中",
			"gameId", gameId, "branchName", branchName, "nodeAgentId", nodeAgentId,
			"status", gc.GetStatus().String())
		return nil
	case nodeagentv1.GameCacheStatus_UNAVAILABLE:
		return errors.New("node game cache is unavailable")
	}

	// 4. 解析 NodeAgent 上的 build_id 用于版本比较。
	//    为空/非数字（如旧版本遗留的无 build_id 记录）→ 视为版本未知，触发下载修正
	nodeBuildId, err := strconv.ParseUint(gc.GetBuildId(), 10, 64)
	if err != nil {
		slog.Warn("[GameCacheManager] 节点 GameCache build_id 为空或非法，触发重新下载",
			"gameId", gameId, "branchName", branchName, "nodeAgentId", nodeAgentId,
			"buildId", gc.GetBuildId(), "status", gc.GetStatus().String())
		return g.triggerDownload(ctx, client, nodeAgentId, gameId, branchName, lastBuildId)
	}

	// 5. 按版本判断是否需要启动下载（至此仅剩 AVAILABLE / REMOVED）
	switch gc.GetStatus() {
	case nodeagentv1.GameCacheStatus_AVAILABLE:
		switch {
		case nodeBuildId < lastBuildId:
			return g.triggerDownload(ctx, client, nodeAgentId, gameId, branchName, lastBuildId)
		case nodeBuildId == lastBuildId:
			// 已是最新且可用
		default:
			return fmt.Errorf("node game cache build_id %d is newer than latest %d", nodeBuildId, lastBuildId)
		}
	case nodeagentv1.GameCacheStatus_REMOVED:
		switch {
		case nodeBuildId <= lastBuildId:
			return g.triggerDownload(ctx, client, nodeAgentId, gameId, branchName, lastBuildId)
		default:
			return fmt.Errorf("node game cache build_id %d is newer than latest %d", nodeBuildId, lastBuildId)
		}
	}

	slog.Info("[GameCacheManager] GameCache 已就绪，无需下载",
		"gameId", gameId, "branchName", branchName, "nodeAgentId", nodeAgentId,
		"buildId", gc.GetBuildId(), "status", gc.GetStatus().String())
	return nil
}

// triggerDownload 调用 NodeAgent 的 CacheGame 启动下载
func (g *GameCacheManager) triggerDownload(ctx context.Context, client *nodeagent.NodeAgentFaceClient,
	nodeAgentId, gameId, branchName string, lastBuildId uint64) error {
	if _, err := client.CacheGame(ctx, &nodeagentv1.CacheGameRequest{
		GameId:     gameId,
		BranchName: branchName,
		BuildId:    strconv.FormatUint(lastBuildId, 10),
	}); err != nil {
		return fmt.Errorf("cache game on node_agent %s: %w", nodeAgentId, err)
	}

	slog.Info("[GameCacheManager] 启动游戏缓存下载",
		"gameId", gameId, "branchName", branchName, "nodeAgentId", nodeAgentId,
		"buildId", lastBuildId)
	return nil
}

// isGameCacheNotFound 判断 NodeAgent 返回的错误是否为“游戏缓存不存在”
// （node_agent 对缺失缓存返回 code=BUILD_CACHE_MISS + category=NOT_FOUND）
func isGameCacheNotFound(detail *nodeagentv1.ErrorDetail) bool {
	if detail == nil {
		return false
	}
	return detail.GetCategory() == nodeagentv1.ErrorCategory_ERROR_CATEGORY_NOT_FOUND ||
		detail.GetCode() == nodeagentv1.BusinessErrorCode_BUSINESS_ERROR_CODE_BUILD_CACHE_MISS
}

// ListNodeBranches 列出某 game 已同步的 Steam 分支记录
func (g *GameCacheManager) ListNodeBranches(ctx context.Context, gameId string) ([]*entity.SteamBranch, error) {
	if gameId == "" {
		return nil, errors.New("game_id is required")
	}
	return g.steamBranchRepo.ListByGame(ctx, gameId)
}

// UpdateBranchCache 按 game_id + branch_name 加载本地分支记录，
// 用其最新构建版本在指定 node_agent 上执行缓存检查，必要时触发下载/更新。
// （CheckAndUpdate 语义：已最新/下载中幂等成功，其他情况返回错误）
func (g *GameCacheManager) UpdateBranchCache(ctx context.Context, gameId, branchName, nodeAgentId string) error {
	branch, err := g.steamBranchRepo.GetByGameAndBranch(ctx, gameId, branchName)
	if err != nil {
		return err
	}
	return g.CheckAndUpdate(ctx, gameId, branchName, branch.LastBuildId, nodeAgentId)
}

// SetBranchMinReplicas 设置分支保底副本数（§4.3，管理员运维）：
// 0 = 按需（实例驱动，需求归零即回收）；N = 无实例也常驻 N 份。
func (g *GameCacheManager) SetBranchMinReplicas(ctx context.Context, gameId, branchName string, minReplicas int) error {
	if minReplicas < 0 {
		return errors.New("min_replicas must be >= 0")
	}
	branch, err := g.steamBranchRepo.GetByGameAndBranch(ctx, gameId, branchName)
	if err != nil {
		return err
	}
	branch.MinReplicas = minReplicas
	branch.UpdateTime = time.Now()
	if err := g.steamBranchRepo.Save(ctx, branch); err != nil {
		return fmt.Errorf("update branch min_replicas: %w", err)
	}
	slog.Info("[GameCacheManager] 设置分支保底副本数", "gameId", gameId,
		"branchName", branchName, "minReplicas", minReplicas)
	return nil
}

// RemoveBranchCache 删除指定 node_agent 上某 (game, branch) 的游戏缓存（释放磁盘）。
// 幂等：节点已无该缓存（GetNodeCache 返回 nil）时直接返回成功；
// 节点上有该 game 的活动实例引用时 node_agent 拒绝删除并返回错误（调用方记日志即可）。
// 触发时机（docs/cache-placement-design.md §7）：分支 Disable/Abandoned、需求归零、
// 磁盘压力、管理员显式删除。
func (g *GameCacheManager) RemoveBranchCache(ctx context.Context, gameId, branchName, nodeAgentId string) error {
	if nodeAgentId == "" {
		return errors.New("node_agent_id is required")
	}
	if gameId == "" || branchName == "" {
		return errors.New("game_id and branch_name are required")
	}

	// 幂等：节点已无缓存 → 无需删除
	gc, err := g.GetNodeCache(ctx, nodeAgentId, gameId, branchName)
	if err != nil {
		return err
	}
	if gc == nil {
		return nil
	}

	nodeAgent, err := g.nodeAgentRepo.GetByID(ctx, nodeAgentId)
	if err != nil {
		return fmt.Errorf("get node_agent %s: %w", nodeAgentId, err)
	}
	node, err := g.nodeRepo.GetByID(nodeAgent.NodeId)
	if err != nil {
		return fmt.Errorf("get node %s: %w", nodeAgent.NodeId, err)
	}
	client, err := g.nodeAgentClients.Get(ctx, nodeAgentId, fmt.Sprintf("%s:%d", node.Ip, nodeAgent.Port))
	if err != nil {
		return fmt.Errorf("get node_agent client %s: %w", nodeAgentId, err)
	}

	resp, err := client.RemoveCache(ctx, &nodeagentv1.RemoveCacheRequest{
		GameId:     gameId,
		BranchName: branchName,
	})
	if err != nil {
		return fmt.Errorf("remove cache on node_agent %s: %w", nodeAgentId, err)
	}

	slog.Info("[GameCacheManager] 删除节点游戏缓存",
		"gameId", gameId, "branchName", branchName, "nodeAgentId", nodeAgentId,
		"removedPath", resp.GetRemovedPath())
	return nil
}

// GetNodeCache 查询指定 node_agent 上某 (game, branch) 的缓存状态。
// 缓存不存在（node_agent 返回 BUILD_CACHE_MISS / NOT_FOUND）时返回 (nil, nil)。
func (g *GameCacheManager) GetNodeCache(ctx context.Context, nodeAgentId, gameId, branchName string) (*nodeagentv1.GameCache, error) {	if nodeAgentId == "" {
		return nil, errors.New("node_agent_id is required")
	}
	if gameId == "" || branchName == "" {
		return nil, errors.New("game_id and branch_name are required")
	}

	nodeAgent, err := g.nodeAgentRepo.GetByID(ctx, nodeAgentId)
	if err != nil {
		return nil, fmt.Errorf("get node_agent %s: %w", nodeAgentId, err)
	}
	node, err := g.nodeRepo.GetByID(nodeAgent.NodeId)
	if err != nil {
		return nil, fmt.Errorf("get node %s: %w", nodeAgent.NodeId, err)
	}
	client, err := g.nodeAgentClients.Get(ctx, nodeAgentId, fmt.Sprintf("%s:%d", node.Ip, nodeAgent.Port))
	if err != nil {
		return nil, fmt.Errorf("get node_agent client %s: %w", nodeAgentId, err)
	}

	resp, err := client.GetCacheGame(ctx, &nodeagentv1.GetCacheGameRequest{
		GameId:     gameId,
		BranchName: branchName,
	})
	if err != nil {
		if isGameCacheNotFound(extractErrorDetail(err)) {
			return nil, nil
		}
		return nil, fmt.Errorf("get cache game from node_agent %s: %w", nodeAgentId, err)
	}
	if resp == nil || resp.GameCache == nil {
		return nil, errors.New("node_agent returned empty game cache")
	}
	return resp.GameCache, nil
}

// CacheAvailable 判断节点 (game, branch) 缓存是否 AVAILABLE（实时 gRPC 查询；
// P2-C 后调度改走 NodeCacheView 快照的 CacheState，本方法保留供运维/兜底）。
func (g *GameCacheManager) CacheAvailable(ctx context.Context, nodeAgentId, gameId, branchName string) (bool, error) {
	gc, err := g.GetNodeCache(ctx, nodeAgentId, gameId, branchName)
	if err != nil {
		return false, err
	}
	if gc == nil {
		return false, nil
	}
	return gc.GetStatus() == nodeagentv1.GameCacheStatus_AVAILABLE, nil
}

// Start 启动后台循环：周期性执行分支同步 + Enable 分支缓存检查/更新。
// interval 每轮间隔；启动后立即执行一轮追平存量。
func (g *GameCacheManager) Start(ctx context.Context, interval time.Duration) {
	slog.Info("[GameCacheManager] 后台循环已启动", "interval", interval.String())
	go g.loop(ctx, interval)
}

func (g *GameCacheManager) loop(ctx context.Context, interval time.Duration) {
	g.reconcileOnce(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("[GameCacheManager] 后台循环退出")
			return
		case <-ticker.C:
			g.reconcileOnce(ctx)
		}
	}
}

// reconcileOnce 执行一轮完整对账（P2-D：从"全扇出"收敛为"diff 增删"，§4.4）：
// 1) 同步分支（SyncAndRecordBranch）
// 2) Disable/Abandoned 分支：全节点删除缓存（P1，释放磁盘）
// 3) Enable 分支：reconcileBranch ——
//    更新已有缓存版本（跟分支最新）→ 删除富余副本（需求归零/min_replicas 外）→ 保底预热
// 单个 game 或单个（分支, 节点）失败只记日志，不中断整轮。
func (g *GameCacheManager) reconcileOnce(ctx context.Context) {
	nodeAgentIDs, err := g.nodeAgentRepo.ListEnabledIDs(ctx)
	if err != nil {
		slog.Error("[GameCacheManager] 查询已启用节点失败", "err", err)
		return
	}
	if len(nodeAgentIDs) == 0 {
		slog.Info("[GameCacheManager] 无已启用节点，跳过本轮")
		return
	}

	games, err := g.gameRepo.ListAll(ctx)
	if err != nil {
		slog.Error("[GameCacheManager] 查询游戏列表失败", "err", err)
		return
	}

	for _, game := range games {
		if err := g.SyncAndRecordBranch(ctx, game.ID); err != nil {
			slog.Error("[GameCacheManager] 分支同步失败", "gameId", game.ID, "err", err)
			continue
		}
		branches, err := g.steamBranchRepo.ListByGame(ctx, game.ID)
		if err != nil {
			slog.Error("[GameCacheManager] 查询分支失败", "gameId", game.ID, "err", err)
			continue
		}
		// demand：该 game 各分支的实例承载节点（引用保护，§4.1）
		serving := g.servingNodesByBranch(ctx, game.ID)
		for _, branch := range branches {
			if branch.Status != entity.Enable {
				// 分支 Disable/Abandoned：从所有已启用节点删除该分支缓存（释放磁盘）。
				// RemoveBranchCache 幂等（节点无缓存即返回），节点上有活动实例引用时拒绝。
				for _, nodeAgentId := range nodeAgentIDs {
					if err := g.RemoveBranchCache(ctx, game.ID, branch.BranchName, nodeAgentId); err != nil {
						slog.Warn("[GameCacheManager] 删除废弃分支缓存失败",
							"gameId", game.ID, "branchName", branch.BranchName,
							"nodeAgentId", nodeAgentId, "err", err)
						continue
					}
				}
				continue
			}
			g.reconcileBranch(ctx, game.ID, branch, nodeAgentIDs, serving[branch.BranchName])
		}
	}
}

// servingNodesByBranch 该 game 各 Enable 分支的"实例承载节点"（demand，§4.1）。
// demand 是派生量：COUNT 实例（含 cache_warming，P2-C 后实例落库 branch_name）。
func (g *GameCacheManager) servingNodesByBranch(ctx context.Context, gameID string) map[string]map[string]bool {
	out := make(map[string]map[string]bool)
	if g.instanceRepo == nil {
		return out
	}
	instances, err := g.instanceRepo.ListByGame(ctx, gameID)
	if err != nil {
		slog.Error("[GameCacheManager] 查询实例失败", "gameId", gameID, "err", err)
		return out
	}
	for _, inst := range instances {
		if inst.NodeAgentID == nil || inst.BranchName == "" || !instanceActiveStatus(inst.Status) {
			continue
		}
		if out[inst.BranchName] == nil {
			out[inst.BranchName] = make(map[string]bool)
		}
		out[inst.BranchName][*inst.NodeAgentID] = true
	}
	return out
}

// instanceActiveStatus 实例是否"占用缓存引用"（demand 口径）
func instanceActiveStatus(s entity.InstanceStatus) bool {
	switch s {
	case entity.StatusCacheWarming, entity.StatusPreparingBuild, entity.StatusRestoringSnapshot,
		entity.StatusStarting, entity.StatusRunning, entity.StatusStopping, entity.StatusCleaning:
		return true
	}
	return false
}

// reconcileBranch 单分支对账（§4.4）：
//  1. 收集各节点实际缓存状态（available/downloading/missing，实时 gRPC）；
//  2. 删除富余：非实例承载的 AVAILABLE 缓存，超出 min_replicas 保底后删除（带冷却防抖）；
//  3. 更新：对实例承载节点 + 保底保留节点的缓存 CheckAndUpdate（跟分支最新）；
//  4. 保底预热：min_replicas > 0 且不足时，挑非缓存节点 CacheGame 补足。
func (g *GameCacheManager) reconcileBranch(ctx context.Context, gameID string, branch *entity.SteamBranch,
	nodeAgentIDs []string, serving map[string]bool) {

	type nodeState struct {
		avail       bool
		downloading bool
	}
	states := make(map[string]nodeState, len(nodeAgentIDs))
	for _, id := range nodeAgentIDs {
		gc, err := g.GetNodeCache(ctx, id, gameID, branch.BranchName)
		if err != nil {
			// 查询失败视为无缓存（保守，下轮重试）
			slog.Warn("[GameCacheManager] 缓存状态查询失败", "gameId", gameID,
				"branchName", branch.BranchName, "nodeAgentId", id, "err", err)
			states[id] = nodeState{}
			continue
		}
		if gc == nil {
			states[id] = nodeState{}
			continue
		}
		switch gc.GetStatus() {
		case nodeagentv1.GameCacheStatus_AVAILABLE:
			states[id] = nodeState{avail: true}
		case nodeagentv1.GameCacheStatus_DOWNLOADING:
			states[id] = nodeState{downloading: true}
		}
	}

	// ---- 2) 删除富余（先删，腾出磁盘再更新/预热）----
	// 保留目标 = max(实例承载节点数, min_replicas 保底)；下载中的不算可用，不删。
	keepTarget := len(serving)
	if branch.MinReplicas > keepTarget {
		keepTarget = branch.MinReplicas
	}
	availCount := 0
	for _, st := range states {
		if st.avail || st.downloading {
			availCount++
		}
	}
	deleted := make(map[string]bool)
	if !g.deleteCooledDown(gameID, branch.BranchName) && availCount > keepTarget {
		for id, st := range states {
			if availCount <= keepTarget {
				break
			}
			if !st.avail || serving[id] {
				continue // 只删"非承载 + 可用"的富余副本
			}
			if err := g.RemoveBranchCache(ctx, gameID, branch.BranchName, id); err != nil {
				slog.Warn("[GameCacheManager] 删除富余缓存失败",
					"gameId", gameID, "branchName", branch.BranchName,
					"nodeAgentId", id, "err", err)
				continue
			}
			deleted[id] = true
			availCount--
			g.markDeleted(gameID, branch.BranchName)
		}
	}

	// ---- 3) 更新：实例承载节点 + 保留的可用缓存节点（CheckAndUpdate 幂等，跟分支最新）----
	for id, st := range states {
		if deleted[id] {
			continue
		}
		if serving[id] || st.avail {
			if err := g.CheckAndUpdate(ctx, gameID, branch.BranchName, branch.LastBuildId, id); err != nil {
				slog.Warn("[GameCacheManager] 缓存检查/更新失败",
					"gameId", gameID, "branchName", branch.BranchName,
					"nodeAgentId", id, "err", err)
				continue
			}
		}
	}

	// ---- 4) 保底预热（§4.3）：min_replicas > 0 且不足时补足 ----
	if branch.MinReplicas > 0 {
		keptAvail := 0
		for id, st := range states {
			if !deleted[id] && (st.avail || st.downloading) {
				keptAvail++
			}
		}
		if keptAvail < branch.MinReplicas {
			for id, st := range states {
				if keptAvail >= branch.MinReplicas {
					break
				}
				if deleted[id] || st.avail || st.downloading {
					continue
				}
				client := g.mustClient(ctx, id)
				if client == nil {
					continue
				}
				if err := g.triggerDownload(ctx, client, id, gameID, branch.BranchName, branch.LastBuildId); err != nil {
					slog.Warn("[GameCacheManager] 保底预热失败",
						"gameId", gameID, "branchName", branch.BranchName,
						"nodeAgentId", id, "err", err)
					continue
				}
				keptAvail++
			}
		}
	}
}

// deleteCooledDown 是否处于删除冷却期（§4.6 防抖）
func (g *GameCacheManager) deleteCooledDown(gameID, branchName string) bool {
	g.deleteMu.Lock()
	defer g.deleteMu.Unlock()
	last, ok := g.deleteCooldown[gameID+":"+branchName]
	return ok && time.Since(last) < cacheDeleteCooldown
}

// markDeleted 记录该分支刚删除过缓存（进入冷却期）
func (g *GameCacheManager) markDeleted(gameID, branchName string) {
	g.deleteMu.Lock()
	defer g.deleteMu.Unlock()
	g.deleteCooldown[gameID+":"+branchName] = time.Now()
}

// mustClient 解析节点客户端（保底预热用；失败返回 nil，调用方跳过）
func (g *GameCacheManager) mustClient(ctx context.Context, nodeAgentID string) *nodeagent.NodeAgentFaceClient {
	nodeAgent, err := g.nodeAgentRepo.GetByID(ctx, nodeAgentID)
	if err != nil {
		slog.Warn("[GameCacheManager] 查询 node_agent 失败", "nodeAgentId", nodeAgentID, "err", err)
		return nil
	}
	node, err := g.nodeRepo.GetByID(nodeAgent.NodeId)
	if err != nil {
		slog.Warn("[GameCacheManager] 查询 node 失败", "nodeId", nodeAgent.NodeId, "err", err)
		return nil
	}
	client, err := g.nodeAgentClients.Get(ctx, nodeAgentID, fmt.Sprintf("%s:%d", node.Ip, nodeAgent.Port))
	if err != nil {
		slog.Warn("[GameCacheManager] 获取 node_agent client 失败", "nodeAgentId", nodeAgentID, "err", err)
		return nil
	}
	return client
}
