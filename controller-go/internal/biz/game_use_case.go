package biz

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"controller-go/internal/client/assetservice"
	"controller-go/internal/entity"
	"controller-go/internal/repository"
	assetservicev1 "controller-go/internal/third/assetservice/v1"
)

// GameUseCase 提供 Game 增删改查，并同步到 asset_service。
// 写操作采用 write-through：先调 asset_service（同步目标必须成功），成功后再落本地库；
// 读操作以本地库为准（controller 是游戏管理的权威入口，本地表驱动实例调度）。
type GameUseCase struct {
	gamerepo       repository.GameRepository
	businessClient *assetservice.BusinessServiceFaceClient
}

func NewGameUseCase(gamerepo repository.GameRepository, businessClient *assetservice.BusinessServiceFaceClient) *GameUseCase {
	return &GameUseCase{gamerepo: gamerepo, businessClient: businessClient}
}

// CreateGame 创建一个 Game：controller 生成 id，先同步到 asset_service，再落本地库。
func (uc *GameUseCase) CreateGame(ctx context.Context, name, appID string) (*entity.Game, error) {
	if name == "" {
		return nil, errors.New("name is required")
	}
	game := &entity.Game{
		ID:    newGameID(),
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

// DeleteGame 删除 Game：先同步到 asset_service（须存在），再删本地库。
func (uc *GameUseCase) DeleteGame(ctx context.Context, id string) error {
	if id == "" {
		return errors.New("id is required")
	}
	if _, err := uc.businessClient.DeleteGame(ctx, &assetservicev1.DeleteGameRequest{Id: id}); err != nil {
		return err
	}
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
