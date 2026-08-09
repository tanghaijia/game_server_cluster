package biz

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
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
}

func NewGameCacheManager(
	nodeAgentClients *nodeagent.ClientRegistry,
	assetClient *assetservice.AssetServiceFaceClient,
	businessClient *assetservice.BusinessServiceFaceClient,
	steamBranchRepo repository.SteamBranchRepository,
	nodeAgentRepo repository.NodeAgentRepository,
	nodeRepo repository.NodeRepository,
) *GameCacheManager {
	return &GameCacheManager{
		nodeAgentClients: nodeAgentClients,
		assetClient:      assetClient,
		businessClient:   businessClient,
		steamBranchRepo:  steamBranchRepo,
		nodeAgentRepo:    nodeAgentRepo,
		nodeRepo:         nodeRepo,
	}
}

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
		return fmt.Errorf("get cache game from node_agent %s: %w", nodeAgentId, err)
	}
	if resp == nil || resp.GameCache == nil {
		return errors.New("node_agent returned empty game cache")
	}
	gc := resp.GameCache

	// 3. 解析 NodeAgent 上的 build_id 用于版本比较
	nodeBuildId, err := strconv.ParseUint(gc.GetBuildId(), 10, 64)
	if err != nil {
		return fmt.Errorf("parse node build_id %q: %w", gc.GetBuildId(), err)
	}

	// 4. 按状态 + 版本判断是否需要启动下载
	shouldDownload := false
	switch gc.GetStatus() {
	case nodeagentv1.GameCacheStatus_DOWNLOADING:
		// 已在下载中 → 幂等成功
	case nodeagentv1.GameCacheStatus_AVAILABLE:
		switch {
		case nodeBuildId < lastBuildId:
			shouldDownload = true
		case nodeBuildId == lastBuildId:
			// 已是最新且可用
		default:
			return fmt.Errorf("node game cache build_id %d is newer than latest %d", nodeBuildId, lastBuildId)
		}
	case nodeagentv1.GameCacheStatus_REMOVED:
		switch {
		case nodeBuildId <= lastBuildId:
			shouldDownload = true
		default:
			return fmt.Errorf("node game cache build_id %d is newer than latest %d", nodeBuildId, lastBuildId)
		}
	case nodeagentv1.GameCacheStatus_UNAVAILABLE:
		return errors.New("node game cache is unavailable")
	default:
		return fmt.Errorf("unexpected node game cache status %s", gc.GetStatus().String())
	}

	if !shouldDownload {
		slog.Info("[GameCacheManager] GameCache 已就绪，无需下载",
			"gameId", gameId, "branchName", branchName, "nodeAgentId", nodeAgentId,
			"buildId", gc.GetBuildId(), "status", gc.GetStatus().String())
		return nil
	}

	// 5. 调用 cachegame 启动下载
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
