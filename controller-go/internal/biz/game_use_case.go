package biz

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"controller-go/internal/client/assetservice"
	"controller-go/internal/entity"
	"controller-go/internal/repository"
	assetservicev1 "controller-go/internal/third/assetservice/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GameUseCase 提供 Game 增删改查，并同步到 asset_service。
// 写操作采用 write-through：先调 asset_service（同步目标必须成功），成功后再落本地库；
// 读操作以本地库为准（controller 是游戏管理的权威入口，本地表驱动实例调度）。
type GameUseCase struct {
	gamerepo            repository.GameRepository
	steamBranchRepo     repository.SteamBranchRepository
	instanceRepo        repository.GameInstanceRepository
	portMappingRepo     repository.ContainerPortMappingRepository
	containerConfigRepo repository.GameContainerConfigRepository
	businessClient      *assetservice.BusinessServiceFaceClient
}

func NewGameUseCase(
	gamerepo repository.GameRepository,
	steamBranchRepo repository.SteamBranchRepository,
	instanceRepo repository.GameInstanceRepository,
	portMappingRepo repository.ContainerPortMappingRepository,
	containerConfigRepo repository.GameContainerConfigRepository,
	businessClient *assetservice.BusinessServiceFaceClient,
) *GameUseCase {
	return &GameUseCase{
		gamerepo:            gamerepo,
		steamBranchRepo:     steamBranchRepo,
		instanceRepo:        instanceRepo,
		portMappingRepo:     portMappingRepo,
		containerConfigRepo: containerConfigRepo,
		businessClient:      businessClient,
	}
}

// CreateGame 创建一个 Game：controller 生成 id，先同步到 asset_service，再落本地库。
func (uc *GameUseCase) CreateGame(ctx context.Context, name, appID string) (*entity.Game, error) {
	if name == "" {
		return nil, errors.New("name is required")
	}
	game := &entity.Game{
		ID:    appID,
		Name:  name,
		AppId: appID,
	}

	// 1) 先同步到 asset_service（同步目标必须成功，失败则不落本地库）
	if _, err := uc.businessClient.CreateGame(ctx, &assetservicev1.CreateGameRequest{
		Game: mapGameToProto(game),
	}); err != nil {
		return nil, err
	}

	// 2) 再落本地库
	if err := uc.gamerepo.Save(ctx, game); err != nil {
		return nil, fmt.Errorf("save game locally: %w", err)
	}
	return game, nil
}

// GetGame 按 id 查询（读本地库）
func (uc *GameUseCase) GetGame(ctx context.Context, id string) (*entity.Game, error) {
	if id == "" {
		return nil, errors.New("id is required")
	}
	return uc.gamerepo.GetByID(ctx, id)
}

// UpdateGame 更新 Game 的 name / app_id：先同步到 asset_service（须存在），再更新本地库。
// 本地保留字段（如 ContainerConfigID）通过先查后改保留。
func (uc *GameUseCase) UpdateGame(ctx context.Context, id, name, appID string) (*entity.Game, error) {
	if id == "" {
		return nil, errors.New("id is required")
	}
	if name == "" {
		return nil, errors.New("name is required")
	}

	// 1) 先同步到 asset_service（须存在，否则报错不落库）
	if _, err := uc.businessClient.UpdateGame(ctx, &assetservicev1.UpdateGameRequest{
		Game: &assetservicev1.Game{Id: id, Name: name, AppId: appID},
	}); err != nil {
		return nil, err
	}

	// 2) 加载本地实体，保留本地字段后更新落库
	existing, err := uc.gamerepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get game locally: %w", err)
	}
	existing.Name = name
	existing.AppId = appID
	if err := uc.gamerepo.Save(ctx, existing); err != nil {
		return nil, fmt.Errorf("save game locally: %w", err)
	}
	return existing, nil
}

// DeleteGame 删除 Game（级联）：
// 1) 存在非终态（运行中/调度中/停止中...）实例 → 拒绝（管理员须先全部停止并删除实例）；
// 2) 级联删除该游戏全部实例及其端口映射；
// 3) 同步删除 asset_service；
// 4) 删除 steam_branches；
// 5) 删除 game_container_configs（仅当无其他游戏引用）；
// 6) 删除 games 行。
func (uc *GameUseCase) DeleteGame(ctx context.Context, id string) error {
	if id == "" {
		return errors.New("id is required")
	}

	// 1) 检查实例状态：非 stopped/failed 的实例拒绝删除
	instances, err := uc.instanceRepo.ListByGame(ctx, id)
	if err != nil {
		return fmt.Errorf("list game instances: %w", err)
	}
	for _, inst := range instances {
		if inst.Status != entity.StatusStopped && inst.Status != entity.Failed {
			return fmt.Errorf("游戏仍有状态为 %s 的实例 %s，请先全部停止并删除实例", inst.Status, inst.ID)
		}
	}

	// 2) 删除实例及其端口映射
	for _, inst := range instances {
		if err := uc.portMappingRepo.DeleteByInstanceId(ctx, inst.ID); err != nil {
			return fmt.Errorf("delete port mappings of instance %s: %w", inst.ID, err)
		}
		if err := uc.instanceRepo.Delete(ctx, inst.ID); err != nil {
			return fmt.Errorf("delete instance %s: %w", inst.ID, err)
		}
	}

	// 3) 同步删除 asset_service；asset_service 中不存在（NotFound）时跳过——
	//    允许删除"本地 seed 但 asset_service 未注册"的游戏
	if _, err := uc.businessClient.DeleteGame(ctx, &assetservicev1.DeleteGameRequest{Id: id}); err != nil {
		if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound {
			slog.Warn("asset_service 中游戏不存在，跳过同步删除", "game_id", id)
		} else {
			return err
		}
	}

	// 4) 删除分支
	if err := uc.steamBranchRepo.DeleteByGame(ctx, id); err != nil {
		return fmt.Errorf("delete steam branches locally: %w", err)
	}

	// 5) 删除容器配置（仅当无其他游戏引用同一配置）
	if game, err := uc.gamerepo.GetByID(ctx, id); err == nil && game.ContainerConfigID != "" {
		if all, err := uc.gamerepo.ListAll(ctx); err == nil {
			referenced := false
			for _, g := range all {
				if g.ID != id && g.ContainerConfigID == game.ContainerConfigID {
					referenced = true
					break
				}
			}
			if !referenced {
				if err := uc.containerConfigRepo.Delete(ctx, game.ContainerConfigID); err != nil {
					return fmt.Errorf("delete container config: %w", err)
				}
			}
		}
	}

	// 6) 删除游戏
	if err := uc.gamerepo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete game locally: %w", err)
	}
	return nil
}

// ListGames 列出全部 Game（读本地库）
func (uc *GameUseCase) ListGames(ctx context.Context) ([]*entity.Game, error) {
	return uc.gamerepo.ListAll(ctx)
}

// mapGameToProto 将本地 Game 实体映射为 asset_service 的 Game 消息（只同步 id/name/app_id）
func mapGameToProto(game *entity.Game) *assetservicev1.Game {
	if game == nil {
		return nil
	}
	return &assetservicev1.Game{
		Id:    game.ID,
		Name:  game.Name,
		AppId: game.AppId,
	}
}

// newGameID 生成唯一 game id
func newGameID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("game-%d", time.Now().UnixNano())
	}
	return "game-" + hex.EncodeToString(b)
}
