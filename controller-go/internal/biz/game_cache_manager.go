package biz

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"controller-go/internal/client/assetservice"
	"controller-go/internal/client/nodeagent"
	"controller-go/internal/entity"
	"controller-go/internal/repository"
	assetservicev1 "controller-go/internal/third/assetservice/v1"

	"gorm.io/gorm"
)

type GameCacheManager struct {
	nodeAgentClients *nodeagent.ClientRegistry
	assetClient      *assetservice.AssetServiceFaceClient
	businessClient   *assetservice.BusinessServiceFaceClient
	steamBranchRepo  repository.SteamBranchRepository
}

func NewGameCacheManager(
	nodeAgentClients *nodeagent.ClientRegistry,
	assetClient *assetservice.AssetServiceFaceClient,
	businessClient *assetservice.BusinessServiceFaceClient,
	steamBranchRepo repository.SteamBranchRepository,
) *GameCacheManager {
	return &GameCacheManager{
		nodeAgentClients: nodeAgentClients,
		assetClient:      assetClient,
		businessClient:   businessClient,
		steamBranchRepo:  steamBranchRepo,
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
